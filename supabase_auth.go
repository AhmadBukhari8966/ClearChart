package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"time"
)

// AuthProvider verifies passwords with the identity provider. Profile roles and
// medical data remain in PostgreSQL; a browser never supplies a trusted user ID.
type AuthProvider interface {
	SignIn(context.Context, string, string) (AuthResult, error)
	SignUp(context.Context, string, string) (AuthResult, error)
}

// AuthResult contains an identity only when Supabase issued an authenticated
// session. A signup awaiting email confirmation returns the zero value instead.
// Provider tokens are deliberately not retained: users sign in again at expiry.
type AuthResult struct {
	UserID    string
	Email     string
	ExpiresIn int
	Confirmed bool
}

var (
	ErrAuthCredentials = errors.New("email or password is incorrect")
	ErrAuthUnconfirmed = errors.New("confirm your email before signing in")
	ErrAuthRateLimited = errors.New("too many authentication attempts; please try again later")
	ErrAuthUnavailable = errors.New("authentication is temporarily unavailable")
	ErrAuthPassword    = errors.New("choose a stronger password that meets the account password requirements")
	ErrAuthSignup      = errors.New("could not create an account; check your details or try signing in")
)

// supabaseAuth exchanges credentials over HTTPS with the project's Auth API.
// Its errors never include upstream bodies, credentials, tokens, or request URLs.
type supabaseAuth struct {
	baseURL string
	key     string
	client  *http.Client
}

// NewSupabaseAuth accepts a project origin, such as https://PROJECT.supabase.co.
// Paths, query strings, and credentials in the URL are rejected to prevent
// accidentally sending passwords to an unrelated endpoint.
func NewSupabaseAuth(projectURL, key string) (*supabaseAuth, error) {
	u, err := url.Parse(strings.TrimSpace(projectURL))
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil ||
		(u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return nil, errors.New("SUPABASE_URL must be an HTTPS project origin without a path or credentials")
	}
	key = strings.TrimSpace(key)
	if key == "" || len(key) > 8192 || strings.ContainsAny(key, "\r\n") {
		return nil, errors.New("a valid Supabase publishable or anon key is required")
	}
	return &supabaseAuth{
		baseURL: strings.TrimRight(u.String(), "/") + "/auth/v1",
		key:     key,
		client: &http.Client{
			Timeout: 10 * time.Second,
			// Never forward credentials or the project key to a redirect target.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}, nil
}

func (a *supabaseAuth) SignIn(ctx context.Context, email, password string) (AuthResult, error) {
	return a.authenticate(ctx, "/token?grant_type=password", email, password, false)
}

func (a *supabaseAuth) SignUp(ctx context.Context, email, password string) (AuthResult, error) {
	return a.authenticate(ctx, "/signup", email, password, true)
}

type supabaseAuthUser struct {
	ID               string `json:"id"`
	Email            string `json:"email"`
	EmailConfirmedAt string `json:"email_confirmed_at"`
}

type supabaseAuthResponse struct {
	// Confirmation-required signup returns a bare user rather than a session.
	supabaseAuthUser
	User        supabaseAuthUser `json:"user"`
	AccessToken string           `json:"access_token"`
	TokenType   string           `json:"token_type"`
	ExpiresIn   int              `json:"expires_in"`
}

func (a *supabaseAuth) authenticate(ctx context.Context, path, email, password string, signup bool) (AuthResult, error) {
	email = strings.TrimSpace(email)
	if !validAuthEmail(email) || len(password) == 0 || len(password) > 4096 {
		if signup {
			return AuthResult{}, ErrAuthSignup
		}
		return AuthResult{}, ErrAuthCredentials
	}
	payload, err := json.Marshal(struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}{email, password})
	if err != nil {
		return AuthResult{}, ErrAuthUnavailable
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return AuthResult{}, ErrAuthUnavailable
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("apikey", a.key)
	res, err := a.client.Do(req)
	if err != nil {
		return AuthResult{}, ErrAuthUnavailable
	}
	defer res.Body.Close()
	const maxAuthBody = 1 << 20
	body, err := io.ReadAll(io.LimitReader(res.Body, maxAuthBody+1))
	if err != nil || len(body) > maxAuthBody {
		return AuthResult{}, ErrAuthUnavailable
	}
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusCreated {
		return AuthResult{}, authResponseError(res.StatusCode, body, signup)
	}
	contentType, _, err := mime.ParseMediaType(res.Header.Get("Content-Type"))
	if err != nil || contentType != "application/json" {
		return AuthResult{}, ErrAuthUnavailable
	}
	var response supabaseAuthResponse
	if json.Unmarshal(body, &response) != nil {
		return AuthResult{}, ErrAuthUnavailable
	}
	if signup && response.AccessToken == "" {
		// Supabase may return an obfuscated user for a duplicate signup. Never
		// use that ID to create an app session or attach an existing profile.
		user := response.User
		if user.ID == "" {
			user = response.supabaseAuthUser
		}
		if !validAuthUserID(user.ID) || !validAuthEmail(user.Email) {
			return AuthResult{}, ErrAuthUnavailable
		}
		return AuthResult{}, nil
	}
	user := response.User
	if response.AccessToken == "" || !strings.EqualFold(response.TokenType, "bearer") ||
		response.ExpiresIn <= 0 || !validAuthUserID(user.ID) || !validAuthEmail(user.Email) {
		return AuthResult{}, ErrAuthUnavailable
	}
	if _, err := time.Parse(time.RFC3339Nano, user.EmailConfirmedAt); err != nil {
		return AuthResult{}, ErrAuthUnconfirmed
	}
	// Keep local sessions bounded even if a project configures very long JWTs.
	if response.ExpiresIn > 24*60*60 {
		response.ExpiresIn = 24 * 60 * 60
	}
	return AuthResult{UserID: user.ID, Email: user.Email, ExpiresIn: response.ExpiresIn, Confirmed: true}, nil
}

// authResponseError uses stable provider error codes, never provider messages.
func authResponseError(status int, body []byte, signup bool) error {
	if status == http.StatusTooManyRequests {
		return ErrAuthRateLimited
	}
	if status >= 500 || status < 400 {
		return ErrAuthUnavailable
	}
	var failure struct {
		Code string `json:"error_code"`
	}
	_ = json.Unmarshal(body, &failure)
	switch failure.Code {
	case "invalid_credentials":
		return ErrAuthCredentials
	case "email_not_confirmed":
		return ErrAuthUnconfirmed
	case "over_request_rate_limit", "over_email_send_rate_limit", "over_sms_send_rate_limit":
		return ErrAuthRateLimited
	case "weak_password":
		return ErrAuthPassword
	case "unexpected_failure", "request_timeout":
		return ErrAuthUnavailable
	}
	if signup {
		return ErrAuthSignup
	}
	// Unknown/configuration errors should not incorrectly blame the password.
	return ErrAuthUnavailable
}

func validAuthEmail(value string) bool {
	if len(value) > 254 || strings.TrimSpace(value) != value {
		return false
	}
	address, err := mail.ParseAddress(value)
	return err == nil && address.Address == value && strings.Contains(value, "@")
}

func validAuthUserID(value string) bool {
	if len(value) != 36 || value == "00000000-0000-0000-0000-000000000000" {
		return false
	}
	for i, c := range value {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
				return false
			}
		}
	}
	return true
}
