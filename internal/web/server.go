// Package web is the presentation layer: HTTP handlers, sessions and templates.
package web

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"infworks/internal/app"
	"infworks/internal/domain"
)

//go:embed templates/*.html static/*
var assets embed.FS

// Users is the identity port (a mock of the UFRGS identity service).
type Users interface {
	Authenticate(ctx context.Context, username, password string) (*domain.User, error)
	User(ctx context.Context, username string) (*domain.User, error)
}

// Documents resolves stored PDF names to file paths.
type Documents interface {
	Path(name string) (string, error)
}

type Server struct {
	Search     *app.Search
	Catalog    *app.Cataloguing
	Moderation *app.Moderation
	Users      Users
	Docs       Documents

	mu       sync.Mutex
	sessions map[string]string // token -> username
	pages    map[string]*template.Template
}

const sessionCookie = "infworks_session"

func New(s *app.Search, c *app.Cataloguing, m *app.Moderation, u Users, d Documents) *Server {
	srv := &Server{Search: s, Catalog: c, Moderation: m, Users: u, Docs: d,
		sessions: map[string]string{}, pages: map[string]*template.Template{}}
	for _, p := range []string{"search", "details", "login", "dashboard", "form", "work", "queue", "error"} {
		srv.pages[p] = template.Must(template.New("").Funcs(funcs).
			ParseFS(assets, "templates/layout.html", "templates/"+p+".html"))
	}
	return srv
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.FileServerFS(assets))
	mux.HandleFunc("GET /{$}", s.handleSearch)
	mux.HandleFunc("GET /works/{id}", s.handleDetails)
	mux.HandleFunc("GET /works/{id}/document", s.handlePublicDocument)
	mux.HandleFunc("GET /login", s.handleLoginPage)
	mux.HandleFunc("POST /login", s.handleLogin)
	mux.HandleFunc("POST /logout", s.handleLogout)
	mux.HandleFunc("GET /staff", s.handleDashboard)
	mux.HandleFunc("GET /staff/works/new", s.handleNewForm)
	mux.HandleFunc("POST /staff/works", s.handleSave)
	mux.HandleFunc("GET /staff/works/{id}", s.handleStaffWork)
	mux.HandleFunc("GET /staff/works/{id}/edit", s.handleEditForm)
	mux.HandleFunc("POST /staff/works/{id}", s.handleSave)
	mux.HandleFunc("GET /staff/works/{id}/document", s.handleStaffDocument)
	mux.HandleFunc("GET /review", s.handleQueue)
	mux.HandleFunc("POST /review/{id}", s.handleDecide)
	return logRequests(mux)
}

// ---- rendering ----

type page struct {
	Title string
	User  *domain.User
	Flash string
	Data  any
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, status int, name, title string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	p := page{Title: title, User: s.currentUser(r), Flash: flashes[r.URL.Query().Get("flash")], Data: data}
	if err := s.pages[name].ExecuteTemplate(w, "layout", p); err != nil {
		log.Printf("render %s: %v", name, err)
	}
}

var flashes = map[string]string{
	"draft":     "Draft saved. Drafts are not publicly visible.",
	"saved":     "Changes saved and recorded in the audit history.",
	"submitted": "Submitted for review. The record is now in the coordinator's queue.",
	"approved":  "Record approved and published.",
	"rejected":  "Record rejected. The reason was sent back to library staff.",
	"login":     "Signed in.",
	"logout":    "Signed out.",
}

type errorData struct {
	Status  int
	Heading string
	Message string
}

// fail maps application errors to HTTP responses (FR-W-03, NFR-03, NFR-06).
func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	var t *app.TransitionError
	switch {
	case errors.Is(err, app.ErrUnauthenticated):
		if r.Method == http.MethodGet {
			http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
			return
		}
		s.errorPage(w, r, http.StatusUnauthorized, "Sign in required", "Please sign in with a staff account to continue.")
	case errors.Is(err, app.ErrForbidden):
		s.errorPage(w, r, http.StatusForbidden, "Access denied", "Your role is not allowed to perform this action.")
	case errors.Is(err, app.ErrNotFound):
		s.errorPage(w, r, http.StatusNotFound, "Not found", "This record does not exist or is not publicly available.")
	case errors.Is(err, app.ErrConflict):
		s.errorPage(w, r, http.StatusConflict, "Record changed",
			"This record was changed or already decided by someone else. Reload it and try again.")
	case errors.As(err, &t):
		s.errorPage(w, r, http.StatusConflict, "Action not permitted",
			fmt.Sprintf("A record in status %s cannot be changed this way.", orNew(t.From)))
	default:
		log.Printf("error: %v", err)
		s.errorPage(w, r, http.StatusServiceUnavailable, "Temporarily unavailable",
			"The service could not complete the request. Nothing was changed; please try again later.")
	}
}

func orNew(st domain.Status) string {
	if st == "" {
		return "NEW"
	}
	return string(st)
}

func (s *Server) errorPage(w http.ResponseWriter, r *http.Request, status int, heading, msg string) {
	s.render(w, r, status, "error", heading, errorData{status, heading, msg})
}

// ---- sessions (mock UFRGS authentication) ----

func (s *Server) currentUser(r *http.Request) *domain.User {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return nil
	}
	s.mu.Lock()
	username, ok := s.sessions[c.Value]
	s.mu.Unlock()
	if !ok {
		return nil
	}
	u, err := s.Users.User(r.Context(), username)
	if err != nil {
		return nil
	}
	return u
}

func (s *Server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	s.render(w, r, http.StatusOK, "login", "Sign in", loginData{Next: r.URL.Query().Get("next")})
}

type loginData struct {
	Next, Username, Error string
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	username, next := strings.TrimSpace(r.FormValue("username")), r.FormValue("next")
	u, err := s.Users.Authenticate(r.Context(), username, r.FormValue("password"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if u == nil {
		s.render(w, r, http.StatusUnauthorized, "login", "Sign in",
			loginData{Next: next, Username: username, Error: "Unknown user or wrong password."})
		return
	}
	b := make([]byte, 24)
	rand.Read(b)
	token := hex.EncodeToString(b)
	s.mu.Lock()
	s.sessions[token] = u.Username
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode})
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") {
		next = "/staff"
		if u.Role == domain.RoleResearcher {
			next = "/"
		}
	}
	http.Redirect(w, r, withFlash(next, "login"), http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		s.mu.Lock()
		delete(s.sessions, c.Value)
		s.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/?flash=logout", http.StatusSeeOther)
}

// ---- helpers ----

func pathID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil && id > 0
}

func withFlash(path, flash string) string {
	if strings.Contains(path, "?") {
		return path + "&flash=" + flash
	}
	return path + "?flash=" + flash
}

func (s *Server) serveDocument(w http.ResponseWriter, r *http.Request, wk domain.Work) {
	p, err := s.Docs.Path(wk.PDFFile)
	if err != nil {
		s.fail(w, r, app.ErrNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", fmt.Sprintf("inf-work-%d.pdf", wk.ID)))
	http.ServeFile(w, r, p)
}

func logRequests(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		h.ServeHTTP(w, r)
		log.Printf("%s %s (%s)", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
	})
}

var funcs = template.FuncMap{
	"join":      func(s []string) string { return strings.Join(s, ", ") },
	"typeLabel": func(t domain.WorkType) string { return t.Label() },
	"statusLabel": func(st domain.Status) string {
		return strings.ReplaceAll(string(st), "_", " ")
	},
	"date": func(t time.Time) string {
		if t.IsZero() {
			return "—"
		}
		return t.Local().Format("2006-01-02 15:04")
	},
	"year": func(y int) string {
		if y == 0 {
			return "—"
		}
		return strconv.Itoa(y)
	},
	"orDash": func(s string) string {
		if strings.TrimSpace(s) == "" {
			return "—"
		}
		return s
	},
	"workTypes": func() []domain.WorkType { return domain.WorkTypes },
	"statuses":  func() []domain.Status { return domain.Statuses },
	"isRole":    func(u *domain.User, role string) bool { return u != nil && string(u.Role) == role },
	"first": func(s []string) string {
		if len(s) == 0 {
			return "—"
		}
		return s[0]
	},
}

// readUpload reads the optional "pdf" file, capped just above the size limit.
func readUpload(r *http.Request) (*app.Upload, error) {
	f, h, err := r.FormFile("pdf")
	if errors.Is(err, http.ErrMissingFile) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if h.Size == 0 && h.Filename == "" {
		return nil, nil
	}
	data, err := io.ReadAll(io.LimitReader(f, domain.MaxPDFBytes+1))
	if err != nil {
		return nil, err
	}
	return &app.Upload{Name: h.Filename, Data: data}, nil
}
