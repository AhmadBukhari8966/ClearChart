package main

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"net/mail"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

// AuthPage is the view model for ordinary HTML login/signup forms. These forms
// use redirects; chart updates continue to use Datastar SSE.
type AuthPage struct{ Mode, CSRF, Email, Error, Message string }

// authConfiguration accepts explicit settings and the existing Next.js-named
// publishable key. Standard Supabase connection URLs identify the same project.
// Neither the database password nor the key is included in returned errors.
func authConfiguration(dsn string) (string, string, error) {
	projectURL := strings.TrimSpace(os.Getenv("SUPABASE_URL"))
	if projectURL == "" {
		if u, err := url.Parse(dsn); err == nil {
			ref := ""
			if m := regexp.MustCompile(`^db\.([a-z0-9]{20})\.supabase\.co$`).FindStringSubmatch(u.Hostname()); len(m) == 2 {
				ref = m[1]
			}
			if ref == "" && strings.HasSuffix(u.Hostname(), ".pooler.supabase.com") && u.User != nil {
				if m := regexp.MustCompile(`^postgres\.([a-z0-9]{20})$`).FindStringSubmatch(u.User.Username()); len(m) == 2 {
					ref = m[1]
				}
			}
			if ref != "" {
				projectURL = "https://" + ref + ".supabase.co"
			}
		}
	}
	key := ""
	for _, name := range []string{"SUPABASE_PUBLISHABLE_KEY", "NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY", "SUPABASE_ANON_KEY"} {
		if key = strings.TrimSpace(os.Getenv(name)); key != "" {
			break
		}
	}
	if projectURL == "" || key == "" {
		return "", "", errors.New("Supabase Auth needs SUPABASE_URL and SUPABASE_PUBLISHABLE_KEY (the existing NEXT_PUBLIC_SUPABASE_PUBLISHABLE_KEY is also accepted)")
	}
	return projectURL, key, nil
}

func (a *app) cookie(w http.ResponseWriter, r *http.Request, name, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", HttpOnly: true,
		Secure: a.secureCookies || r.TLS != nil, SameSite: http.SameSiteLaxMode, MaxAge: maxAge})
}

// issueSession rotates the opaque local cookie after authentication/onboarding.
// Only a verified Auth result may supply identity; only PostgreSQL supplies role.
func (a *app) issueSession(w http.ResponseWriter, r *http.Request, s session) session {
	s.CSRF = newID()
	token := newID()
	a.mu.Lock()
	if old, err := r.Cookie("clearchart_session"); err == nil {
		delete(a.sessions, old.Value)
	}
	for key, old := range a.sessions {
		if !time.Now().Before(old.Expires) {
			delete(a.sessions, key)
		}
	}
	a.sessions[token] = s
	a.mu.Unlock()
	a.cookie(w, r, "clearchart_session", token, max(1, int(time.Until(s.Expires).Seconds())))
	return s
}

func (a *app) currentSession(r *http.Request) (session, bool) {
	c, err := r.Cookie("clearchart_session")
	if err != nil {
		return session{}, false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	s, ok := a.sessions[c.Value]
	if ok && !time.Now().Before(s.Expires) {
		delete(a.sessions, c.Value)
		ok = false
	}
	return s, ok
}

func (a *app) sessionFor(r *http.Request, role string) (session, bool) {
	s, ok := a.currentSession(r)
	return s, ok && s.ProfileID != "" && validRole(role) && s.Role == role
}

func sessionDestination(s session) string {
	if s.ProfileID == "" {
		return "/onboard/patient"
	}
	return "/dashboard/" + s.Role + "/" + s.ProfileID
}

func (a *app) home(w http.ResponseWriter, r *http.Request) {
	dest := "/login"
	if s, ok := a.currentSession(r); ok {
		dest = sessionDestination(s)
	}
	http.Redirect(w, r, dest, http.StatusSeeOther)
}

func (a *app) authForm(w http.ResponseWriter, r *http.Request) {
	if s, ok := a.currentSession(r); ok {
		http.Redirect(w, r, sessionDestination(s), http.StatusSeeOther)
		return
	}
	mode := "login"
	if r.URL.Path == "/signup" {
		mode = "signup"
	}
	a.authPage(w, r, AuthPage{Mode: mode}, http.StatusOK)
}

func (a *app) authPage(w http.ResponseWriter, r *http.Request, data AuthPage, status int) {
	data.CSRF = newID()
	a.cookie(w, r, "clearchart_auth_csrf", data.CSRF, 3600)
	body, err := a.render("auth.html", data)
	if err != nil {
		http.Error(w, "The sign-in page could not be loaded.", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

func (a *app) login(w http.ResponseWriter, r *http.Request)  { a.authenticate(w, r, false) }
func (a *app) signup(w http.ResponseWriter, r *http.Request) { a.authenticate(w, r, true) }

func (a *app) authenticate(w http.ResponseWriter, r *http.Request, signup bool) {
	mode := "login"
	if signup {
		mode = "signup"
	}
	data := AuthPage{Mode: mode}
	if err := parseForm(w, r); err != nil {
		data.Error = "Please shorten the form fields."
		a.authPage(w, r, data, 400)
		return
	}
	c, err := r.Cookie("clearchart_auth_csrf")
	if err != nil || c.Value == "" || !sameOrigin(r) || subtle.ConstantTimeCompare([]byte(c.Value), []byte(r.PostForm.Get("csrf"))) != 1 {
		data.Error = "This form expired. Please try again."
		a.authPage(w, r, data, 403)
		return
	}
	data.Email = strings.ToLower(strings.TrimSpace(r.PostForm.Get("email")))
	password := r.PostForm.Get("password")
	address, err := mail.ParseAddress(data.Email)
	if err != nil || address.Address != data.Email || len(data.Email) > 254 || len(password) == 0 || len(password) > 4096 || (signup && len(password) < 8) {
		data.Error = "Enter a valid email address and password. New passwords need at least 8 characters."
		a.authPage(w, r, data, 400)
		return
	}
	if a.auth == nil {
		data.Error = "Sign-in is not configured. Contact the workspace administrator."
		a.authPage(w, r, data, 503)
		return
	}
	var result AuthResult
	if signup {
		result, err = a.auth.SignUp(r.Context(), data.Email, password)
	} else {
		result, err = a.auth.SignIn(r.Context(), data.Email, password)
	}
	if err != nil {
		status := http.StatusBadRequest
		switch {
		case errors.Is(err, ErrAuthCredentials):
			data.Error = ErrAuthCredentials.Error()
			status = 401
		case errors.Is(err, ErrAuthUnconfirmed):
			data.Error = ErrAuthUnconfirmed.Error()
			status = 401
		case errors.Is(err, ErrAuthRateLimited):
			data.Error = ErrAuthRateLimited.Error()
			status = 429
		case errors.Is(err, ErrAuthPassword):
			data.Error = ErrAuthPassword.Error()
		case errors.Is(err, ErrAuthSignup):
			data.Error = ErrAuthSignup.Error()
		default:
			data.Error = ErrAuthUnavailable.Error()
			status = 503
		}
		a.authPage(w, r, data, status)
		return
	}
	if signup && !result.Confirmed {
		data.Mode = "login"
		data.Message = "Check your inbox to confirm your email, then return here to sign in. If you already have an account, sign in with your existing password."
		a.authPage(w, r, data, 200)
		return
	}
	if !result.Confirmed || result.UserID == "" || result.Email == "" || result.ExpiresIn <= 0 {
		data.Error = ErrAuthUnavailable.Error()
		a.authPage(w, r, data, 503)
		return
	}
	s := session{AuthUserID: result.UserID, Email: result.Email, Expires: time.Now().Add(time.Duration(min(result.ExpiresIn, 3600)) * time.Second)}
	p, err := a.store.ProfileByAuthUserID(r.Context(), s.AuthUserID)
	if err == nil {
		s.ProfileID, s.Role = p.ID, p.Role
	} else if !errors.Is(err, ErrNotFound) {
		data.Error = "Your account was verified, but your profile could not be loaded. Please try again."
		a.authPage(w, r, data, 503)
		return
	}
	a.issueSession(w, r, s)
	a.cookie(w, r, "clearchart_auth_csrf", "", -1)
	http.Redirect(w, r, sessionDestination(s), http.StatusSeeOther)
}

// Logout invalidates this browser's local session. No provider tokens are stored.
func (a *app) logout(w http.ResponseWriter, r *http.Request) {
	if err := parseForm(w, r); err != nil {
		http.Error(w, "Invalid form.", 400)
		return
	}
	s, ok := a.currentSession(r)
	if !ok {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if !sameOrigin(r) || subtle.ConstantTimeCompare([]byte(s.CSRF), []byte(r.PostForm.Get("csrf"))) != 1 {
		http.Error(w, "This form expired. Refresh the page and try again.", 403)
		return
	}
	c, _ := r.Cookie("clearchart_session")
	a.mu.Lock()
	delete(a.sessions, c.Value)
	a.mu.Unlock()
	a.cookie(w, r, "clearchart_session", "", -1)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
