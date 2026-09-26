package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type fakeAuthProvider struct {
	result AuthResult
	err    error
	calls  int
}

func (f *fakeAuthProvider) SignIn(context.Context, string, string) (AuthResult, error) {
	f.calls++
	return f.result, f.err
}
func (f *fakeAuthProvider) SignUp(context.Context, string, string) (AuthResult, error) {
	f.calls++
	return f.result, f.err
}
func authFormCookies(t *testing.T, h http.Handler, mode string) ([]*http.Cookie, string) {
	t.Helper()
	w := serveRequest(h, httptest.NewRequest("GET", "/"+mode, nil), nil)
	if w.Code != 200 {
		t.Fatalf("auth page %d: %s", w.Code, w.Body.String())
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == "clearchart_auth_csrf" {
			return w.Result().Cookies(), c.Value
		}
	}
	t.Fatal("missing CSRF cookie")
	return nil, ""
}
func TestSignedOutRoutesAndRemovedDemo(t *testing.T) {
	_, h := testApp(t)
	for _, path := range []string{"/", "/dashboard/patient/" + demoPatientID, "/onboard/doctor"} {
		w := serveRequest(h, httptest.NewRequest("GET", path, nil), nil)
		if w.Code != 303 || w.Header().Get("Location") != "/login" {
			t.Fatalf("%s: %d %s", path, w.Code, w.Header().Get("Location"))
		}
	}
	for _, path := range []string{"/demo/patient", "/demo/doctor?id=" + demoDoctorID} {
		if w := serveRequest(h, httptest.NewRequest("GET", path, nil), nil); w.Code != 404 {
			t.Fatalf("demo route accessible: %d", w.Code)
		}
	}
	for _, mode := range []string{"login", "signup"} {
		authFormCookies(t, h, mode)
	}
}
func TestAuthenticationUsesVerifiedIdentityAndRotatesSession(t *testing.T) {
	for _, existing := range []bool{false, true} {
		t.Run(map[bool]string{false: "new", true: "existing"}[existing], func(t *testing.T) {
			a, h := testApp(t)
			id := newID()
			f := &fakeAuthProvider{result: AuthResult{UserID: id, Email: demoDoctorEmail, Confirmed: true, ExpiresIn: 7200}}
			a.auth = f
			if existing {
				s := a.store.(*memoryStore)
				p := s.profiles[demoDoctorID]
				p.AuthUserID = id
				s.profiles[p.ID] = p
			}
			cookies, csrf := authFormCookies(t, h, "login")
			w := postForm(h, "/login", url.Values{"csrf": {csrf}, "email": {demoDoctorEmail}, "password": {"password123"}, "role": {"patient"}, "auth_user_id": {newID()}, "profile_id": {demoPatientID}}, cookies)
			if w.Code != 303 {
				t.Fatalf("login %d %s", w.Code, w.Body.String())
			}
			r := httptest.NewRequest("GET", "/", nil)
			for _, c := range w.Result().Cookies() {
				r.AddCookie(c)
			}
			s, ok := a.currentSession(r)
			if !ok || s.AuthUserID != id || s.Email != demoDoctorEmail || time.Until(s.Expires) > time.Hour {
				t.Fatal("invalid verified session")
			}
			if existing {
				if s.Role != "doctor" || s.ProfileID != demoDoctorID {
					t.Fatal("did not use stored identity")
				}
			} else if s.ProfileID != "" || w.Header().Get("Location") != "/onboard/patient" {
				t.Fatal("claimed seeded account by email")
			}
			fresh := httptest.NewRecorder()
			a.issueSession(fresh, r, s)
			if _, ok := a.currentSession(r); ok {
				t.Fatal("old cookie survives rotation")
			}
		})
	}
}
func TestPendingSignupAndAuthFailuresNeverSignIn(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
	}{
		{"pending", nil, 200}, {"password", ErrAuthPassword, 400}, {"credentials", ErrAuthCredentials, 401}, {"unconfirmed", ErrAuthUnconfirmed, 401}, {"limit", ErrAuthRateLimited, 429}, {"internal", errors.New("secret upstream body"), 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, h := testApp(t)
			a.auth = &fakeAuthProvider{err: tc.err}
			cookies, csrf := authFormCookies(t, h, "signup")
			w := postForm(h, "/signup", url.Values{"csrf": {csrf}, "email": {"new@example.com"}, "password": {"secret-password"}}, cookies)
			if w.Code != tc.status {
				t.Fatalf("got %d: %s", w.Code, w.Body.String())
			}
			if len(a.sessions) != 0 || strings.Contains(w.Body.String(), "secret-password") || strings.Contains(w.Body.String(), "secret upstream body") {
				t.Fatal("authentication leaked information or granted session")
			}
		})
	}
}
func TestAuthCSRFCrossOriginAndLogout(t *testing.T) {
	a, h := testApp(t)
	f := &fakeAuthProvider{}
	a.auth = f
	cookies, csrf := authFormCookies(t, h, "login")
	for _, cross := range []bool{false, true} {
		values := url.Values{"csrf": {"wrong"}, "email": {"new@example.com"}, "password": {"password123"}}
		if cross {
			values.Set("csrf", csrf)
		}
		r := httptest.NewRequest("POST", "/login", strings.NewReader(values.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if cross {
			r.Header.Set("Origin", "https://evil.example")
		}
		if w := serveRequest(h, r, cookies); w.Code != 403 {
			t.Fatalf("bad CSRF status %d", w.Code)
		}
	}
	if f.calls != 0 {
		t.Fatal("invalid request reached provider")
	}
	sessionCookies, s, _ := demoSession(t, a, h, "doctor")
	if w := postForm(h, "/logout", url.Values{"csrf": {"wrong"}}, sessionCookies); w.Code != 403 {
		t.Fatal("logout accepted invalid CSRF")
	}
	w := postForm(h, "/logout", url.Values{"csrf": {s.CSRF}}, sessionCookies)
	if w.Code != 303 || w.Header().Get("Location") != "/login" {
		t.Fatal("logout failed")
	}
	r := httptest.NewRequest("GET", "/", nil)
	for _, c := range sessionCookies {
		r.AddCookie(c)
	}
	if _, ok := a.currentSession(r); ok {
		t.Fatal("logged-out token still valid")
	}
	a.secureCookies = true
	out := httptest.NewRecorder()
	a.issueSession(out, r, session{Expires: time.Now().Add(-time.Second)})
	for _, c := range out.Result().Cookies() {
		if !c.Secure || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode {
			t.Fatal("cookie protections missing")
		}
	}
	expired := httptest.NewRequest("GET", "/", nil)
	for _, c := range out.Result().Cookies() {
		expired.AddCookie(c)
	}
	if _, ok := a.currentSession(expired); ok {
		t.Fatal("expired session accepted")
	}
}
func TestAuthConfigurationAliasesAndSafeErrors(t *testing.T) {
	for _, name := range []string{"SUPABASE_URL", "SUPABASE_PUBLISHABLE_KEY", "NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY", "SUPABASE_ANON_KEY"} {
		t.Setenv(name, "")
	}
	ref := "abcdefghijklmnopqrst"
	t.Setenv("NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY", "test-key")
	for _, dsn := range []string{"postgres://postgres:secret@db." + ref + ".supabase.co/postgres", "postgres://postgres." + ref + ":secret@aws-0-test.pooler.supabase.com/postgres"} {
		u, k, e := authConfiguration(dsn)
		if e != nil || u != "https://"+ref+".supabase.co" || k != "test-key" {
			t.Fatal("failed to infer known project")
		}
	}
	if _, _, err := authConfiguration("postgres://postgres:private-password@localhost/postgres"); err == nil || strings.Contains(err.Error(), "private-password") {
		t.Fatal("missing safe config error")
	}
	t.Setenv("SUPABASE_URL", "https://custom.example")
	t.Setenv("SUPABASE_PUBLISHABLE_KEY", "preferred")
	u, k, e := authConfiguration("")
	if e != nil || u != "https://custom.example" || k != "preferred" {
		t.Fatal("explicit config ignored")
	}
}
