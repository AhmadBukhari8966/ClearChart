package main

import (
	"context"
	"net/http"
	"time"
)

// Notification is a single in-app notification item shown in the center.
type Notification struct {
	ID, Kind, Title, Body, Link string
	CreatedAt                   time.Time
	Invitation                  Invitation // populated for invitation-kind items
}

type notificationPage struct {
	Profile       Profile
	CSRF          string
	Notifications []Notification
	Count         int
}

// ─── store methods ───────────────────────────────────────────────────────────

// PendingInvitationsForEmail returns active (pending, not expired) invitations
// addressed to the given email, newest first.
func (s *postgresStore) PendingInvitationsForEmail(ctx context.Context, email string) ([]Invitation, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT i.id::text, i.doctor_id::text, d.name, i.email, i.status,
		       COALESCE(i.patient_id::text,''), i.created_at, i.expires_at
		FROM   care_invitations i
		JOIN   profiles d ON d.id = i.doctor_id
		WHERE  lower(i.email) = lower($1)
		  AND  i.status = 'pending'
		  AND  i.expires_at > now()
		ORDER  BY i.created_at DESC
		LIMIT  50`, email)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Invitation
	for rows.Next() {
		var i Invitation
		if err = rows.Scan(&i.ID, &i.DoctorID, &i.DoctorName, &i.Email, &i.Status, &i.PatientID, &i.CreatedAt, &i.ExpiresAt); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// RecentInvitationActivity returns invitation responses (accepted/declined) for a
// doctor in the last 30 days, newest first.
func (s *postgresStore) RecentInvitationActivity(ctx context.Context, doctorID string) ([]Invitation, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT i.id::text, i.doctor_id::text, COALESCE(p.name, ''), i.email, i.status,
		       COALESCE(i.patient_id::text,''), i.created_at, i.expires_at
		FROM   care_invitations i
		LEFT   JOIN profiles p ON p.id = i.patient_id
		WHERE  i.doctor_id = $1
		  AND  i.status IN ('accepted','declined')
		  AND  i.responded_at >= now() - interval '30 days'
		ORDER  BY i.responded_at DESC
		LIMIT  50`, doctorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Invitation
	for rows.Next() {
		var i Invitation
		if err = rows.Scan(&i.ID, &i.DoctorID, &i.DoctorName, &i.Email, &i.Status, &i.PatientID, &i.CreatedAt, &i.ExpiresAt); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// ─── helper ──────────────────────────────────────────────────────────────────

func buildNotifications(invitations []Invitation, role string) []Notification {
	var out []Notification
	for _, inv := range invitations {
		n := Notification{ID: inv.ID, CreatedAt: inv.CreatedAt, Invitation: inv}
		switch role {
		case "patient":
			n.Kind = "invite"
			n.Title = inv.DoctorName + " wants to connect"
			n.Body = "You have a pending care invitation. Accept to share your chart with this doctor."
			n.Link = "" // responded inline
		case "doctor":
			if inv.Status == "accepted" {
				n.Kind = "accepted"
				name := inv.Email
				if inv.DoctorName != "" {
					name = inv.DoctorName
				}
				n.Title = name + " accepted your invitation"
				n.Body = "Their chart is now shared with you. Open your dashboard to view it."
			} else {
				n.Kind = "declined"
				n.Title = inv.Email + " declined your invitation"
				n.Body = "They chose not to share their chart at this time."
			}
		}
		out = append(out, n)
	}
	return out
}

// ─── handler ─────────────────────────────────────────────────────────────────

func (a *app) notificationsPanel(w http.ResponseWriter, r *http.Request) {
	s, ok := a.currentSession(r)
	if !ok || s.ProfileID == "" {
		http.Error(w, "Session required.", 403)
		return
	}
	var invitations []Invitation
	var err error
	switch s.Role {
	case "patient":
		invitations, err = a.store.PendingInvitationsForEmail(r.Context(), s.Email)
	case "doctor":
		invitations, err = a.store.RecentInvitationActivity(r.Context(), s.ProfileID)
	}
	if err != nil {
		http.Error(w, "Could not load notifications.", 500)
		return
	}
	notifications := buildNotifications(invitations, s.Role)
	// The panel templates do not render the profile, so it is not loaded.
	data := notificationPage{
		CSRF:          s.CSRF,
		Notifications: notifications,
		Count:         len(notifications),
	}
	startSSE(w)
	if fragment, err := a.render("notification-panel", data); err == nil {
		patch(w, "#notification-panel", "outer", fragment)
	}
	if fragment, err := a.render("notification-badge", data); err == nil {
		patch(w, "#notification-badge", "outer", fragment)
	}
}
