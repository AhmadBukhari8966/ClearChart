package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestInvitationIntegrationCheck(t *testing.T) {
	ctx := context.Background()
	admin, e := sql.Open("postgres", "postgres://clearchart_test@127.0.0.1:55432/postgres?sslmode=disable")
	if e != nil {
		t.Fatal(e)
	}
	defer admin.Close()
	name := "invite_check_" + strings.ReplaceAll(newID(), "-", "")
	if _, e = admin.Exec("CREATE DATABASE " + name); e != nil {
		t.Fatal(e)
	}
	defer func() {
		if _, e := admin.Exec("DROP DATABASE " + name + " WITH (FORCE)"); e != nil {
			t.Error(e)
		}
	}()
	st, e := NewPostgresStore(ctx, "postgres://clearchart_test@127.0.0.1:55432/"+name+"?sslmode=disable")
	if e != nil {
		t.Fatal(e)
	}
	defer st.Close()
	s := st.(*postgresStore)
	for _, file := range []string{"schema.sql", "migrations/002_care_invitations.sql"} {
		b, e := os.ReadFile(file)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = s.db.Exec(string(b)); e != nil {
			t.Fatal(e)
		}
	}
	doctor, e := s.CreateProfile(ctx, Profile{AuthUserID: newID(), Role: "doctor", Name: "Doctor Test", Email: "doctor@example.org", LicenseNum: "TEST-123", Specialization: "Primary care"})
	if e != nil {
		t.Fatal(e)
	}
	patient, e := s.CreateProfile(ctx, Profile{AuthUserID: newID(), Role: "patient", Name: "Patient Test", Email: "patient@example.org", DOB: "1990-01-01", BloodType: "O+"})
	if e != nil {
		t.Fatal(e)
	}
	a, e := newApp(s, "Test")
	if e != nil {
		t.Fatal(e)
	}
	h := a.routes()
	sessionForProfile := func(p Profile) ([]*http.Cookie, session) {
		w := httptest.NewRecorder()
		v := a.issueSession(w, httptest.NewRequest("GET", "/", nil), session{AuthUserID: p.AuthUserID, Email: p.Email, ProfileID: p.ID, Role: p.Role, Expires: time.Now().Add(time.Hour)})
		return w.Result().Cookies(), v
	}
	dc, ds := sessionForProfile(doctor)
	pc, ps := sessionForProfile(patient)
	request := func(method, path string, v url.Values, cs []*http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(v.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		for _, c := range cs {
			r.AddCookie(c)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	if w := request("GET", "/invitations", nil, dc); w.Code != 200 || !strings.Contains(w.Body.String(), "Invite a patient") {
		t.Fatal("doctor invitation page", w.Body.String())
	}
	if w := request("POST", "/invitations", url.Values{"csrf": {"wrong"}, "email": {patient.Email}}, dc); !strings.Contains(w.Body.String(), "expired") {
		t.Fatal("CSRF allowed")
	}
	list, _ := s.Invitations(ctx, doctor.ID)
	if len(list) != 0 {
		t.Fatal("CSRF created invite")
	}
	w := request("POST", "/invitations", url.Values{"csrf": {ds.CSRF}, "email": {patient.Email}}, dc)
	if !strings.Contains(w.Body.String(), "Invitation created") || strings.Contains(w.Body.String(), "ZgotmplZ") {
		t.Fatal(w.Body.String())
	}
	list, _ = s.Invitations(ctx, doctor.ID)
	if len(list) != 1 {
		t.Fatal("no saved invitation")
	}
	// Parse token from the escaped HTML link returned by the Datastar fragment.
	body := w.Body.String()
	pos := strings.Index(body, "http://example.com/invitations/")
	if pos < 0 {
		t.Fatal(body)
	}
	token := body[pos+len("http://example.com/invitations/"):]
	token = token[:64]
	if !validInvitationToken(token) {
		t.Fatal("invalid link")
	}
	path := "/invitations/" + token
	signedOut := request("GET", path, nil, nil)
	if signedOut.Code != 303 || signedOut.Header().Get("Location") != "/login" {
		t.Fatal("not gated")
	}
	var pending *http.Cookie
	for _, c := range signedOut.Result().Cookies() {
		if c.Name == "clearchart_invitation" {
			pending = c
		}
	}
	if pending == nil {
		t.Fatal("invitation not saved through login")
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(pending)
	if a.invitationDestination(r, ps) != path {
		t.Fatal("login continuation lost")
	}
	review := request("GET", path, nil, pc)
	if review.Code != 200 || !strings.Contains(review.Body.String(), "Accept and share my chart") {
		t.Fatal(review.Body.String())
	}
	if linked, _ := s.IsCareTeam(ctx, patient.ID, doctor.ID); linked {
		t.Fatal("GET granted access")
	}
	if err := s.RespondInvitation(ctx, invitationHash(token), patient.ID, "other@example.org", true); !errors.Is(err, ErrForbidden) {
		t.Fatal("wrong email accepted", err)
	}
	if err := s.RespondInvitation(ctx, invitationHash(token), doctor.ID, patient.Email, true); !errors.Is(err, ErrForbidden) {
		t.Fatal("doctor accepted", err)
	}
	accepted := request("POST", path, url.Values{"csrf": {ps.CSRF}, "decision": {"accept"}}, pc)
	if !strings.Contains(accepted.Body.String(), "Invitation accepted") {
		t.Fatal(accepted.Body.String())
	}
	if linked, _ := s.IsCareTeam(ctx, patient.ID, doctor.ID); !linked {
		t.Fatal("accept did not create care link")
	}
	if err := s.RespondInvitation(ctx, invitationHash(token), patient.ID, patient.Email, true); err != nil {
		t.Fatal("accept replay not idempotent", err)
	}
	if err := s.RevokeInvitation(ctx, list[0].ID, doctor.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("revoked accepted invitation")
	}
	for _, state := range []string{"declined", "revoked", "expired"} {
		hash := invitationHash(newID())
		i, e := s.CreateInvitation(ctx, doctor.ID, patient.Email, hash)
		if e != nil {
			t.Fatal(e)
		}
		switch state {
		case "declined":
			e = s.RespondInvitation(ctx, hash, patient.ID, patient.Email, false)
		case "revoked":
			if e = s.RevokeInvitation(ctx, i.ID, patient.ID); !errors.Is(e, ErrNotFound) {
				t.Fatal("foreign revoke")
			}
			e = s.RevokeInvitation(ctx, i.ID, doctor.ID)
		case "expired":
			_, e = s.db.Exec("UPDATE care_invitations SET expires_at=now()-interval '1 minute' WHERE id=$1", i.ID)
		}
		if e != nil {
			t.Fatal(e)
		}
		if e = s.RespondInvitation(ctx, hash, patient.ID, patient.Email, true); !errors.Is(e, ErrConflict) {
			t.Fatal(state, e)
		}
	}
	for _, p := range []Profile{doctor, patient} {
		cs, _ := sessionForProfile(p)
		page := request("GET", "/dashboard/"+p.Role+"/"+p.ID, nil, cs)
		if page.Code != 200 || strings.Contains(page.Body.String(), "ZgotmplZ") {
			t.Fatal("dashboard", page.Body.String())
		}
	}
}
