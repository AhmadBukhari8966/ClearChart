package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const authTestUserID = "bac8a978-ff22-42ee-9d12-d3a1848ea81a"

func fakeSupabaseAuth(t *testing.T, handler http.HandlerFunc) *supabaseAuth {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	client, err := NewSupabaseAuth(server.URL, "test-publishable-key")
	if err != nil {
		t.Fatal(err)
	}
	client.client.Transport = server.Client().Transport
	return client
}

func authSessionJSON(id, email string, expires int) string {
	return fmt.Sprintf(`{"access_token":"test-token","token_type":"bearer","expires_in":%d,"user":{"id":%q,"email":%q,"email_confirmed_at":"2026-09-26T00:00:00Z"}}`, expires, id, email)
}

func TestSupabaseAuthPasswordExchange(t *testing.T) {
	for _, signup := range []bool{false, true} {
		t.Run(fmt.Sprintf("signup_%t", signup), func(t *testing.T) {
			client := fakeSupabaseAuth(t, func(w http.ResponseWriter, r *http.Request) {
				wantURI := "/auth/v1/token?grant_type=password"
				if signup {
					wantURI = "/auth/v1/signup"
				}
				if r.Method != http.MethodPost || r.URL.RequestURI() != wantURI {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.RequestURI())
				}
				if r.Header.Get("apikey") != "test-publishable-key" || r.Header.Get("Content-Type") != "application/json" {
					t.Error("missing API key or JSON header")
				}
				var credentials map[string]string
				if err := json.NewDecoder(r.Body).Decode(&credentials); err != nil {
					t.Error(err)
				}
				if len(credentials) != 2 || credentials["email"] != "alex@example.test" || credentials["password"] != "a password with spaces " {
					t.Error("credentials were missing or password was modified")
				}
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				fmt.Fprint(w, authSessionJSON(authTestUserID, "alex@example.test", 3600))
			})
			var result AuthResult
			var err error
			if signup {
				result, err = client.SignUp(context.Background(), " alex@example.test ", "a password with spaces ")
			} else {
				result, err = client.SignIn(context.Background(), " alex@example.test ", "a password with spaces ")
			}
			if err != nil || !result.Confirmed || result.UserID != authTestUserID || result.Email != "alex@example.test" || result.ExpiresIn != 3600 {
				t.Fatalf("unexpected authentication result: %+v, %v", result, err)
			}
		})
	}
}

func TestSupabaseSignupConfirmationNeverAuthenticates(t *testing.T) {
	for _, payload := range []string{
		`{"id":"` + authTestUserID + `","email":"alex@example.test","email_confirmed_at":null}`,
		`{"user":{"id":"` + authTestUserID + `","email":"alex@example.test","email_confirmed_at":"2026-09-26T00:00:00Z"}}`,
	} {
		client := fakeSupabaseAuth(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, payload)
		})
		result, err := client.SignUp(context.Background(), "alex@example.test", "password123")
		if err != nil || result != (AuthResult{}) {
			t.Fatalf("pending or duplicate signup must not provide an identity: %+v %v", result, err)
		}
	}
}

func TestSupabaseAuthRejectsUnsafeResponses(t *testing.T) {
	cases := []struct {
		name, contentType, body string
		want                    error
	}{
		{"malformed_json", "application/json", `{"access_token":`, ErrAuthUnavailable},
		{"empty_json", "application/json", `{}`, ErrAuthUnavailable},
		{"null_json", "application/json", `null`, ErrAuthUnavailable},
		{"html_response", "text/html", authSessionJSON(authTestUserID, "alex@example.test", 3600), ErrAuthUnavailable},
		{"missing_token", "application/json", `{"user":{"id":"` + authTestUserID + `","email":"alex@example.test"}}`, ErrAuthUnavailable},
		{"invalid_id", "application/json", authSessionJSON("not-a-uuid", "alex@example.test", 3600), ErrAuthUnavailable},
		{"zero_id", "application/json", authSessionJSON("00000000-0000-0000-0000-000000000000", "alex@example.test", 3600), ErrAuthUnavailable},
		{"invalid_email", "application/json", authSessionJSON(authTestUserID, "Alex <alex@example.test>", 3600), ErrAuthUnavailable},
		{"expired", "application/json", authSessionJSON(authTestUserID, "alex@example.test", 0), ErrAuthUnavailable},
		{"unconfirmed", "application/json", strings.ReplaceAll(authSessionJSON(authTestUserID, "alex@example.test", 3600), "2026-09-26T00:00:00Z", ""), ErrAuthUnconfirmed},
		{"oversized", "application/json", strings.Repeat(" ", (1<<20)+1), ErrAuthUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := fakeSupabaseAuth(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				fmt.Fprint(w, tc.body)
			})
			result, err := client.SignIn(context.Background(), "alex@example.test", "password123")
			if !errors.Is(err, tc.want) || result != (AuthResult{}) {
				t.Fatalf("unsafe response authenticated: %+v %v", result, err)
			}
		})
	}
}

func TestSupabaseAuthErrorsDoNotExposeProviderDetails(t *testing.T) {
	cases := []struct {
		name, code string
		status     int
		signup     bool
		want       error
	}{
		{"password", "invalid_credentials", 400, false, ErrAuthCredentials},
		{"confirmation", "email_not_confirmed", 400, false, ErrAuthUnconfirmed},
		{"rate_status", "", 429, false, ErrAuthRateLimited},
		{"rate_code", "over_email_send_rate_limit", 422, true, ErrAuthRateLimited},
		{"password_policy", "weak_password", 422, true, ErrAuthPassword},
		{"signup", "email_address_invalid", 422, true, ErrAuthSignup},
		{"server", "", 503, false, ErrAuthUnavailable},
		{"configuration", "bad_jwt", 401, false, ErrAuthUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := fakeSupabaseAuth(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				fmt.Fprintf(w, `{"error_code":%q,"msg":"private-password-123 private-project-key"}`, tc.code)
			})
			var err error
			if tc.signup {
				_, err = client.SignUp(context.Background(), "alex@example.test", "private-password-123")
			} else {
				_, err = client.SignIn(context.Background(), "alex@example.test", "private-password-123")
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("wrong error: %v", err)
			}
			if strings.Contains(err.Error(), "private-") {
				t.Fatal("provider detail escaped into error")
			}
		})
	}
}

func TestSupabaseAuthDoesNotFollowCredentialRedirect(t *testing.T) {
	var redirected atomic.Bool
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirected.Store(true) }))
	defer target.Close()
	client := fakeSupabaseAuth(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	})
	if _, err := client.SignIn(context.Background(), "alex@example.test", "password123"); !errors.Is(err, ErrAuthUnavailable) {
		t.Fatalf("redirect should fail safely: %v", err)
	}
	if redirected.Load() {
		t.Fatal("credentials were sent to a redirected endpoint")
	}
}

func TestSupabaseAuthConfigurationAndCancellation(t *testing.T) {
	for _, origin := range []string{"", "http://project.supabase.co", "https://user:secret@project.supabase.co", "https://project.supabase.co/auth/v1", "https://project.supabase.co?key=secret", "https://project.supabase.co#fragment"} {
		if _, err := NewSupabaseAuth(origin, "test-key"); err == nil {
			t.Errorf("accepted invalid origin %q", origin)
		}
	}
	for _, key := range []string{"", "key\r\ninjected-header: value"} {
		if _, err := NewSupabaseAuth("https://project.supabase.co", key); err == nil {
			t.Error("accepted invalid key")
		}
	}
	client := fakeSupabaseAuth(t, func(w http.ResponseWriter, r *http.Request) { t.Error("cancelled or invalid request reached upstream") })
	if client.client.Timeout != 10*time.Second {
		t.Error("authentication requests must have a bounded timeout")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.SignIn(ctx, "alex@example.test", "password123"); !errors.Is(err, ErrAuthUnavailable) {
		t.Fatalf("cancelled request: %v", err)
	}
	if _, err := client.SignIn(context.Background(), "Alex <alex@example.test>", "password123"); !errors.Is(err, ErrAuthCredentials) {
		t.Fatalf("invalid email: %v", err)
	}
}

func TestSupabaseAuthCapsLocalSessionDuration(t *testing.T) {
	client := fakeSupabaseAuth(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, authSessionJSON(authTestUserID, "alex@example.test", 604800))
	})
	result, err := client.SignIn(context.Background(), "alex@example.test", "password123")
	if err != nil || result.ExpiresIn != 86400 {
		t.Fatalf("unbounded session duration: %+v %v", result, err)
	}
}
