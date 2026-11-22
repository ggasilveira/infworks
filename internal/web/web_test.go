package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"infworks/internal/app"
	"infworks/internal/store"
)

func newTestServer(t *testing.T) (*httptest.Server, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	docs, _ := store.NewFileStore(filepath.Join(dir, "docs"))
	if err := db.SeedIfEmpty(context.Background(), docs, time.Now()); err != nil {
		t.Fatal(err)
	}
	srv := New(&app.Search{Repo: db, Now: time.Now}, &app.Cataloguing{Repo: db, Docs: docs, Now: time.Now},
		&app.Moderation{Repo: db, Now: time.Now}, db, docs)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts, db
}

func client(t *testing.T, ts *httptest.Server, user string) *http.Client {
	t.Helper()
	c := ts.Client()
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	if user == "" {
		return c
	}
	res, err := c.PostForm(ts.URL+"/login", url.Values{"username": {user}, "password": {user}})
	if err != nil || res.StatusCode != http.StatusSeeOther {
		t.Fatalf("login %s: %v %v", user, err, res)
	}
	c.Jar = &jar{cookies: res.Cookies()}
	return c
}

type jar struct{ cookies []*http.Cookie }

func (j *jar) SetCookies(*url.URL, []*http.Cookie) {}
func (j *jar) Cookies(*url.URL) []*http.Cookie     { return j.cookies }

func countWorks(t *testing.T, db *store.Store) int {
	var n int
	db.DB.QueryRow(`SELECT COUNT(*) FROM works`).Scan(&n)
	return n
}

// TW-19 / AC-03e / E4: direct requests to write endpoints are refused server-side.
func TestTW19_DirectWriteRequests(t *testing.T) {
	ts, db := newTestServer(t)
	before := countWorks(t, db)
	form := url.Values{"title": {"Sneaky"}, "action": {"draft"}}
	cases := []struct {
		user, path string
		want       int
	}{
		{"", "/staff/works", http.StatusUnauthorized},
		{"researcher", "/staff/works", http.StatusForbidden},
		{"coordinator", "/staff/works", http.StatusForbidden},
		{"researcher", "/staff/works/1", http.StatusForbidden},
		{"researcher", "/review/26", http.StatusForbidden},
		{"library", "/review/26", http.StatusForbidden},
	}
	for _, c := range cases {
		res, err := client(t, ts, c.user).PostForm(ts.URL+c.path, form)
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != c.want {
			t.Errorf("%q POST %s: got %d, want %d", c.user, c.path, res.StatusCode, c.want)
		}
	}
	if countWorks(t, db) != before {
		t.Error("unauthorized requests changed data")
	}
	var st string
	db.DB.QueryRow(`SELECT status FROM works WHERE id = 26`).Scan(&st)
	if st != "UNDER_REVIEW" {
		t.Errorf("work 26 status changed to %s", st)
	}
}

// Anonymous GET of a staff page redirects to sign in.
func TestStaffPageRedirectsToLogin(t *testing.T) {
	ts, _ := newTestServer(t)
	res, _ := client(t, ts, "").Get(ts.URL + "/staff")
	if res.StatusCode != http.StatusSeeOther || !strings.HasPrefix(res.Header.Get("Location"), "/login") {
		t.Fatalf("got %d %s", res.StatusCode, res.Header.Get("Location"))
	}
}

// TS-04..TS-06 over HTTP: 200 characters pass, 201 are rejected with 422.
func TestTS06_HTTPLengthValidation(t *testing.T) {
	ts, _ := newTestServer(t)
	for n, want := range map[int]int{200: http.StatusOK, 201: http.StatusUnprocessableEntity} {
		res, _ := http.Get(ts.URL + "/?q=" + strings.Repeat("a", n))
		if res.StatusCode != want {
			t.Errorf("%d chars: got %d, want %d", n, res.StatusCode, want)
		}
	}
}

// AC-01e / FR-S-11: non-published details are 404 publicly.
func TestNonPublishedDetailsHidden(t *testing.T) {
	ts, _ := newTestServer(t)
	for _, id := range []string{"25", "26", "27"} { // draft, under review, rejected seeds
		res, _ := http.Get(ts.URL + "/works/" + id)
		if res.StatusCode != http.StatusNotFound {
			t.Errorf("work %s: got %d", id, res.StatusCode)
		}
	}
}

// TS-12 over HTTP: database outage gives 503 and the unavailable message.
func TestTS12_HTTPUnavailable(t *testing.T) {
	ts, db := newTestServer(t)
	db.Close()
	res, _ := http.Get(ts.URL + "/?q=graph")
	if res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("got %d", res.StatusCode)
	}
}
