package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Invitation struct {
	ID, DoctorID, DoctorName, Email, Status, PatientID string
	CreatedAt, ExpiresAt                               time.Time
}
type invitationPage struct {
	Profile                    Profile
	CSRF, Token, Link, Message string
	Invitation                 Invitation
	Invitations                []Invitation
	CanRespond                 bool
}

func invitationHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
func validInvitationToken(token string) bool {
	b, err := hex.DecodeString(token)
	return err == nil && len(b) == 32
}

func (s *postgresStore) CreateInvitation(ctx context.Context, doctor, email, hash string) (Invitation, error) {
	var i Invitation
	err := s.db.QueryRowContext(ctx, `INSERT INTO care_invitations(doctor_id,email,token_hash)
 SELECT id,lower($2),$3 FROM profiles WHERE id=$1 AND role='doctor' AND auth_user_id IS NOT NULL
 RETURNING id::text,doctor_id::text,email,status,created_at,expires_at`, doctor, email, hash).Scan(&i.ID, &i.DoctorID, &i.Email, &i.Status, &i.CreatedAt, &i.ExpiresAt)
	return i, storageError(err)
}
func (s *postgresStore) Invitations(ctx context.Context, doctor string) ([]Invitation, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id::text,email,CASE WHEN status='pending' AND expires_at<=now() THEN 'expired' ELSE status END,created_at,expires_at FROM care_invitations WHERE doctor_id=$1 ORDER BY created_at DESC LIMIT 100`, doctor)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Invitation
	for rows.Next() {
		var i Invitation
		if err = rows.Scan(&i.ID, &i.Email, &i.Status, &i.CreatedAt, &i.ExpiresAt); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}
func (s *postgresStore) Invitation(ctx context.Context, hash string) (Invitation, error) {
	var i Invitation
	err := s.db.QueryRowContext(ctx, `SELECT i.id::text,i.doctor_id::text,d.name,i.email,
 CASE WHEN i.status='pending' AND i.expires_at<=now() THEN 'expired' ELSE i.status END,
 COALESCE(i.patient_id::text,''),i.created_at,i.expires_at
 FROM care_invitations i JOIN profiles d ON d.id=i.doctor_id WHERE i.token_hash=$1`, hash).Scan(&i.ID, &i.DoctorID, &i.DoctorName, &i.Email, &i.Status, &i.PatientID, &i.CreatedAt, &i.ExpiresAt)
	return i, storageError(err)
}
func (s *postgresStore) InvitationByID(ctx context.Context, id string) (Invitation, error) {
	var i Invitation
	err := s.db.QueryRowContext(ctx, `SELECT i.id::text,i.doctor_id::text,d.name,i.email,
 CASE WHEN i.status='pending' AND i.expires_at<=now() THEN 'expired' ELSE i.status END,
 COALESCE(i.patient_id::text,''),i.created_at,i.expires_at
 FROM care_invitations i JOIN profiles d ON d.id=i.doctor_id WHERE i.id=$1`, id).Scan(&i.ID, &i.DoctorID, &i.DoctorName, &i.Email, &i.Status, &i.PatientID, &i.CreatedAt, &i.ExpiresAt)
	return i, storageError(err)
}

// Lock the invitation so acceptance, declining, revocation, and replay cannot
// produce conflicting outcomes. Care access is granted only in this transaction.
func (s *postgresStore) RespondInvitation(ctx context.Context, hash, patient, email string, accept bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id, doctor, target, status, owner string
	var expired bool
	err = tx.QueryRowContext(ctx, `SELECT id::text,doctor_id::text,email,status,COALESCE(patient_id::text,''),expires_at<=now() FROM care_invitations WHERE token_hash=$1 FOR UPDATE`, hash).Scan(&id, &doctor, &target, &status, &owner, &expired)
	if err != nil {
		return storageError(err)
	}
	if !strings.EqualFold(target, email) {
		return ErrForbidden
	}
	var eligible bool
	err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM profiles WHERE id=$1 AND role='patient' AND auth_user_id IS NOT NULL)`, patient).Scan(&eligible)
	if err != nil {
		return err
	}
	if !eligible {
		return ErrForbidden
	}
	if status == "accepted" && owner == patient && accept {
		return tx.Commit()
	}
	if status != "pending" || expired {
		return ErrConflict
	}
	status = "declined"
	if accept {
		status = "accepted"
		if _, err = tx.ExecContext(ctx, `INSERT INTO care_team(patient_id,doctor_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, patient, doctor); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE care_invitations SET status=$2,patient_id=$3,responded_at=now() WHERE id=$1`, id, status, patient); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *postgresStore) RevokeInvitation(ctx context.Context, id, doctor string) error {
	result, err := s.db.ExecContext(ctx, `UPDATE care_invitations SET status='revoked',responded_at=now() WHERE id=$1 AND doctor_id=$2 AND status='pending'`, id, doctor)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (a *app) invitationDestination(r *http.Request, s session) string {
	if s.ProfileID != "" {
		if c, err := r.Cookie("clearchart_invitation"); err == nil && validInvitationToken(c.Value) {
			return "/invitations/" + c.Value
		}
	}
	return sessionDestination(s)
}
func (a *app) invitationsPage(w http.ResponseWriter, r *http.Request) {
	s, ok := a.sessionFor(r, "doctor")
	if !ok {
		http.Redirect(w, r, "/login", 303)
		return
	}
	p, err := a.store.Profile(r.Context(), s.ProfileID)
	if err != nil {
		a.readError(w, err)
		return
	}
	list, err := a.store.Invitations(r.Context(), s.ProfileID)
	if err != nil {
		a.readError(w, err)
		return
	}
	a.page(w, "invitations.html", invitationPage{Profile: p, CSRF: s.CSRF, Invitations: list})
}
func (a *app) createInvitation(w http.ResponseWriter, r *http.Request) {
	if parseForm(w, r) != nil {
		a.feedback(w, "Invalid form.", 400)
		return
	}
	s, ok := a.authorizePost(w, r, "doctor")
	if !ok {
		return
	}
	email := strings.ToLower(strings.TrimSpace(r.PostForm.Get("email")))
	if !validAuthEmail(email) {
		a.feedback(w, "Enter the patient's account email address.", 400)
		return
	}
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		a.feedback(w, "Could not create invitation.", 500)
		return
	}
	token := hex.EncodeToString(secret[:])
	i, err := a.store.CreateInvitation(r.Context(), s.ProfileID, email, invitationHash(token))
	if err != nil {
		a.feedback(w, "Could not save invitation. Check the database migration.", 500)
		return
	}
	scheme := "http"
	if r.TLS != nil || a.secureCookies {
		scheme = "https"
	}
	link := (&url.URL{Scheme: scheme, Host: r.Host, Path: "/invitations/" + token}).String()
	fragment, err := a.render("invitation-created", invitationPage{Invitation: i, Link: link})
	if err != nil {
		a.feedback(w, "Invitation saved. Create another link if needed.", 500)
		return
	}
	startSSE(w)
	patch(w, "#invitation-result", "outer", fragment)
	a.invitationList(w, r, s)
	patch(w, "#form-feedback", "outer", feedbackHTML("Invitation created. Share the link with your patient; no email has been sent.", false))
	// Notify the patient in real-time if they are online.
	if p, err := a.store.ProfileByEmail(r.Context(), email); err == nil {
		a.hub.publish(p.ID, "")
	}
}
func (a *app) invitationList(w http.ResponseWriter, r *http.Request, s session) {
	list, err := a.store.Invitations(r.Context(), s.ProfileID)
	if err != nil {
		return
	}
	if fragment, err := a.render("invitation-list", invitationPage{CSRF: s.CSRF, Invitations: list}); err == nil {
		patch(w, "#invitation-list", "outer", fragment)
	}
}

// respondInvitationInline lets a patient accept or decline an invitation directly
// from the notification center, using the invitation's UUID instead of the full token URL.
// The token_hash is never sent to the client; the DB lookup is purely server-side.
func (a *app) respondInvitationInline(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Referrer-Policy", "no-referrer")
	if parseForm(w, r) != nil {
		a.feedback(w, "Invalid form.", 400)
		return
	}
	s, ok := a.authorizePost(w, r, "patient")
	if !ok {
		return
	}
	if !sameOrigin(r) || subtle.ConstantTimeCompare([]byte(s.CSRF), []byte(r.PostForm.Get("csrf"))) != 1 {
		a.feedback(w, "Refresh this form and try again.", 403)
		return
	}
	invID := r.PathValue("id")
	choice := r.PostForm.Get("decision")
	if choice != "accept" && choice != "decline" {
		a.feedback(w, "Invalid invitation response.", 400)
		return
	}
	// Look up the invitation to get its token_hash without exposing the token.
	inv, err := a.store.InvitationByID(r.Context(), invID)
	if err != nil {
		a.feedback(w, "Invitation not found or already handled.", 404)
		return
	}
	// Verify the patient is the intended recipient.
	if !strings.EqualFold(s.Email, inv.Email) {
		a.feedback(w, "This invitation is addressed to a different account.", 403)
		return
	}
	// Re-derive hash from InvitationByID result — we don't store the hash on the struct,
	// but we can use the invitation's ID to look up the token_hash directly.
	var tokenHash string
	if err = a.store.(*postgresStore).db.QueryRowContext(r.Context(),
		`SELECT token_hash FROM care_invitations WHERE id=$1`, invID).Scan(&tokenHash); err != nil {
		a.feedback(w, "Invitation not found.", 404)
		return
	}
	if err = a.store.RespondInvitation(r.Context(), tokenHash, s.ProfileID, s.Email, choice == "accept"); err != nil {
		message := "This invitation is expired, already handled, or unavailable."
		if errors.Is(err, ErrForbidden) {
			message = "This invitation does not match your patient account."
		}
		a.feedback(w, message, 400)
		return
	}
	message := "Invitation declined."
	if choice == "accept" {
		message = "Invitation accepted. Your doctor is now on your care team."
		a.hub.publish(s.ProfileID, "")
	}
	a.hub.publish(inv.DoctorID, "")
	// Refresh the notification panel.
	a.notificationsPanel(w, r)
	patch(w, "#form-feedback", "outer", feedbackHTML(message, false))
}
func (a *app) revokeInvitation(w http.ResponseWriter, r *http.Request) {
	if parseForm(w, r) != nil {
		a.feedback(w, "Invalid form.", 400)
		return
	}
	s, ok := a.authorizePost(w, r, "doctor")
	if !ok {
		return
	}
	if err := a.store.RevokeInvitation(r.Context(), r.PathValue("id"), s.ProfileID); err != nil {
		a.feedback(w, "Invitation is no longer pending or does not belong to you.", 400)
		return
	}
	startSSE(w)
	a.invitationList(w, r, s)
	patch(w, "#invitation-result", "outer", `<div id="invitation-result"></div>`)
	patch(w, "#form-feedback", "outer", feedbackHTML("Invitation revoked. Its link can no longer grant access.", false))
}
func (a *app) reviewInvitation(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if !validInvitationToken(token) {
		http.NotFound(w, r)
		return
	}
	s, ok := a.currentSession(r)
	if !ok {
		a.cookie(w, r, "clearchart_invitation", token, 7*24*3600)
		http.Redirect(w, r, "/login", 303)
		return
	}
	if s.ProfileID == "" {
		a.cookie(w, r, "clearchart_invitation", token, 7*24*3600)
		http.Redirect(w, r, "/onboard/patient", 303)
		return
	}
	a.cookie(w, r, "clearchart_invitation", "", -1)
	i, err := a.store.Invitation(r.Context(), invitationHash(token))
	if err != nil {
		a.readError(w, err)
		return
	}
	p, err := a.store.Profile(r.Context(), s.ProfileID)
	if err != nil {
		a.readError(w, err)
		return
	}
	data := invitationPage{Profile: p, CSRF: s.CSRF, Token: token, Invitation: i}
	if s.Role != "patient" {
		data.Message = "This invitation is for a patient account. Sign out and sign in as the patient, then reopen this link."
	} else if !strings.EqualFold(s.Email, i.Email) {
		data.Message = "This invitation is addressed to a different email. Sign in with the account your doctor invited and reopen this link."
	} else if i.Status != "pending" {
		data.Message = "This invitation is " + i.Status + ". Ask the doctor for a new link if needed."
	} else {
		data.CanRespond = true
	}
	a.page(w, "invitation-review.html", data)
}
func (a *app) respondInvitation(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Referrer-Policy", "no-referrer")
	if parseForm(w, r) != nil {
		a.feedback(w, "Invalid form.", 400)
		return
	}
	s, ok := a.authorizePost(w, r, "patient")
	if !ok {
		return
	}
	if !sameOrigin(r) || subtle.ConstantTimeCompare([]byte(s.CSRF), []byte(r.PostForm.Get("csrf"))) != 1 {
		a.feedback(w, "Refresh this form and try again.", 403)
		return
	}
	token := r.PathValue("token")
	choice := r.PostForm.Get("decision")
	if !validInvitationToken(token) || (choice != "accept" && choice != "decline") {
		a.feedback(w, "Invalid invitation response.", 400)
		return
	}
	err := a.store.RespondInvitation(r.Context(), invitationHash(token), s.ProfileID, s.Email, choice == "accept")
	if err != nil {
		message := "This invitation is expired, already handled, or unavailable."
		if errors.Is(err, ErrForbidden) {
			message = "This invitation does not match your patient account."
		}
		a.feedback(w, message, 400)
		return
	}
	a.cookie(w, r, "clearchart_invitation", "", -1)
	message := "Invitation declined. No access was granted."
	if choice == "accept" {
		message = "Invitation accepted. Your doctor is now on your care team."
		a.hub.publish(s.ProfileID, "")
	}
	// Notify the doctor that a response arrived (accepted or declined).
	if inv, err := a.store.Invitation(r.Context(), invitationHash(token)); err == nil {
		a.hub.publish(inv.DoctorID, "")
	}
	startSSE(w)
	fragment, _ := a.render("invitation-complete", invitationPage{Profile: Profile{ID: s.ProfileID, Role: s.Role}, Message: message})
	patch(w, "#invitation-decision", "outer", fragment)
}
