package main

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"

	"github.com/joho/godotenv"
)

//go:embed templates/*.html static/*
var assets embed.FS

type session struct {
	AuthUserID, Email, ProfileID, Role, CSRF string
	Expires                                  time.Time
}

type app struct {
	store         Store
	templates     *template.Template
	mode          string
	mu            sync.Mutex
	sessions      map[string]session
	hub           *eventHub
	auth          AuthProvider
	secureCookies bool
}

func newApp(store Store, mode string) (*app, error) {
	t, err := template.New("").Funcs(template.FuncMap{
		"initials": func(s string) string {
			var out []rune
			for _, p := range strings.Fields(strings.TrimPrefix(s, "Dr. ")) {
				out = append(out, []rune(p)[0])
				if len(out) == 2 {
					break
				}
			}
			return string(out)
		},
		"date":      func(t time.Time) string { return t.Format("January 2, 2006") },
		"shortDate": func(t time.Time) string { return t.Format("Jan 2") },
		"clock":     func(t time.Time) string { return t.Format("3:04 PM") },
		"percent":   func(v int) int { return v * 10 },
		"label": func(s string) string {
			labels := map[string]string{"note": "Clinical note", "prescription": "Prescription", "imaging": "Imaging", "pain": "Pain level", "mobility": "Mobility", "energy": "Energy"}
			if v, ok := labels[s]; ok {
				return v
			}
			return s
		},
		"notificationPage": func(count int) notificationPage { return notificationPage{Count: count} },
	}).ParseFS(assets, "templates/*.html")
	if err != nil {
		return nil, err
	}
	return &app{store: store, templates: t, mode: mode, sessions: make(map[string]session), hub: newEventHub()}, nil
}

func (a *app) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", a.home)
	mux.HandleFunc("GET /login", a.authForm)
	mux.HandleFunc("POST /login", a.login)
	mux.HandleFunc("GET /signup", a.authForm)
	mux.HandleFunc("POST /signup", a.signup)
	mux.HandleFunc("POST /logout", a.logout)
	mux.HandleFunc("GET /invitations", a.invitationsPage)
	mux.HandleFunc("POST /invitations", a.createInvitation)
	mux.HandleFunc("GET /invitations/{token}", a.reviewInvitation)
	mux.HandleFunc("POST /invitations/{token}", a.respondInvitation)
	mux.HandleFunc("POST /invitations/{id}/revoke", a.revokeInvitation)
	mux.HandleFunc("GET /notifications", a.notificationsPanel)
	mux.HandleFunc("POST /respond-invitation/{id}", a.respondInvitationInline)
	mux.HandleFunc("GET /dashboard/{role}/{id}", a.dashboard)
	mux.HandleFunc("GET /onboard/{role}", a.onboardForm)
	mux.HandleFunc("POST /onboard/{role}", a.onboard)
	mux.HandleFunc("POST /add-record", a.addRecord)
	mux.HandleFunc("POST /upload-report", a.uploadReport)
	mux.HandleFunc("POST /healing", a.addHealing)
	mux.HandleFunc("GET /events/{role}/{id}", a.events)
	mux.HandleFunc("GET /mock-scans/{id}/{scan}", a.mockScan)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintln(w, "ok")
	})
	static, _ := fs.Sub(assets, "static")
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(static))))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Frame-Options", "DENY")
		if !strings.HasPrefix(r.URL.Path, "/static/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		mux.ServeHTTP(w, r)
	})
}

func (a *app) render(name string, data any) (string, error) {
	var buf bytes.Buffer
	err := a.templates.ExecuteTemplate(&buf, name, data)
	return buf.String(), err
}

func (a *app) page(w http.ResponseWriter, name string, data any) {
	html, err := a.render(name, data)
	if err != nil {
		log.Printf("render %s: %v", name, err)
		http.Error(w, "The page could not be loaded.", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, html)
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		log.Fatal("Could not read .env. Check its formatting and permissions.")
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL is required. Configure Supabase in .env or the process environment.")
	}
	projectURL, key, err := authConfiguration(dsn)
	if err != nil {
		log.Fatal(err)
	}
	auth, err := NewSupabaseAuth(projectURL, key)
	if err != nil {
		log.Fatal("Invalid Supabase Auth configuration. Check SUPABASE_URL and SUPABASE_PUBLISHABLE_KEY.")
	}
	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	store, err := NewPostgresStore(connectCtx, dsn)
	cancel()
	if err != nil {
		log.Fatal("Database connection failed. Check DATABASE_URL; connection details were not logged.")
	}
	defer store.Close()
	checkCtx, checkCancel := context.WithTimeout(ctx, 5*time.Second)
	_, err = store.ProfileByAuthUserID(checkCtx, "00000000-0000-0000-0000-000000000000")
	checkCancel()
	if err != nil && !errors.Is(err, ErrNotFound) {
		log.Fatal("Database schema is not ready. Apply migrations/001_auth_identity.sql to the existing database (schema.sql for a new database).")
	}
	invitationCtx, invitationCancel := context.WithTimeout(ctx, 5*time.Second)
	_, err = store.Invitations(invitationCtx, "00000000-0000-0000-0000-000000000000")
	invitationCancel()
	if err != nil {
		log.Fatal("Invitation schema is not ready. Apply migrations/002_care_invitations.sql (schema.sql for a new database).")
	}
	a, err := newApp(store, "Supabase connected")
	if err != nil {
		log.Fatal(err)
	}
	a.auth = auth
	switch strings.ToLower(os.Getenv("COOKIE_SECURE")) {
	case "", "false":
	case "true":
		a.secureCookies = true
	default:
		log.Fatal("COOKIE_SECURE must be true or false.")
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	host := os.Getenv("HOST")
	if host == "" {
		host = "127.0.0.1"
	}
	srv := &http.Server{Addr: net.JoinHostPort(host, port), Handler: a.routes(), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
	go func() {
		<-ctx.Done()
		a.hub.close()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdownCtx)
	}()
	log.Printf("ClearChart ? Supabase connected ? http://%s ? sign in to continue", srv.Addr)
	if err = srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
