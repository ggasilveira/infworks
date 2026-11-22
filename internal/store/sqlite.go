// Package store implements the persistence and document storage adapters.
package store

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"database/sql/driver"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"modernc.org/sqlite"

	"infworks/internal/app"
	"infworks/internal/domain"
)

const schema = `
PRAGMA foreign_keys = ON;
CREATE TABLE IF NOT EXISTS users (
	username      TEXT PRIMARY KEY,
	display_name  TEXT NOT NULL,
	role          TEXT NOT NULL CHECK (role IN ('LIBRARY_WORKER','COORDINATOR','RESEARCHER')),
	password_hash TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS works (
	id             INTEGER PRIMARY KEY AUTOINCREMENT,
	title          TEXT    NOT NULL DEFAULT '',
	year           INTEGER NOT NULL DEFAULT 0,
	type           TEXT    NOT NULL DEFAULT '',
	status         TEXT    NOT NULL CHECK (status IN ('DRAFT','UNDER_REVIEW','PUBLISHED','REJECTED')),
	advisor        TEXT    NOT NULL DEFAULT '',
	abstract       TEXT    NOT NULL DEFAULT '',
	shelf_location TEXT    NOT NULL DEFAULT '',
	external_url   TEXT    NOT NULL DEFAULT '',
	pdf_file       TEXT    NOT NULL DEFAULT '',
	pdf_name       TEXT    NOT NULL DEFAULT '',
	contact_info   TEXT    NOT NULL DEFAULT '',
	contact_public INTEGER NOT NULL DEFAULT 0,
	version        INTEGER NOT NULL DEFAULT 1,
	created_by     TEXT    NOT NULL DEFAULT '',
	created_at     TEXT    NOT NULL,
	updated_at     TEXT    NOT NULL
);
CREATE INDEX IF NOT EXISTS works_status_year ON works (status, year);
-- authors, keywords and research areas, kept in order
CREATE TABLE IF NOT EXISTS work_terms (
	work_id  INTEGER NOT NULL REFERENCES works (id) ON DELETE CASCADE,
	kind     TEXT    NOT NULL CHECK (kind IN ('author','keyword','area')),
	position INTEGER NOT NULL,
	value    TEXT    NOT NULL,
	PRIMARY KEY (work_id, kind, position)
);
CREATE TABLE IF NOT EXISTS review_decisions (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	work_id    INTEGER NOT NULL REFERENCES works (id),
	decision   TEXT    NOT NULL CHECK (decision IN ('APPROVED','REJECTED')),
	reason     TEXT    NOT NULL DEFAULT '',
	moderator  TEXT    NOT NULL,
	decided_at TEXT    NOT NULL,
	CHECK (decision = 'APPROVED' OR trim(reason) <> '')
);
CREATE TABLE IF NOT EXISTS audit_entries (
	id      INTEGER PRIMARY KEY AUTOINCREMENT,
	work_id INTEGER NOT NULL REFERENCES works (id),
	actor   TEXT    NOT NULL,
	action  TEXT    NOT NULL,
	detail  TEXT    NOT NULL DEFAULT '',
	at      TEXT    NOT NULL
);
`

var registerOnce sync.Once

// registerNorm exposes domain.Normalize to SQL so search ignores case,
// accents and spacing exactly like the duplicate rule does.
func registerNorm() {
	registerOnce.Do(func() {
		_ = sqlite.RegisterDeterministicScalarFunction("norm", 1,
			func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
				s, _ := args[0].(string)
				return domain.Normalize(s), nil
			})
	})
}

type Store struct{ DB *sql.DB }

var _ app.Repository = (*Store)(nil)

func Open(path string) (*Store, error) {
	registerNorm()
	db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("create schema: %w", err)
	}
	return &Store{DB: db}, nil
}

func (s *Store) Close() error { return s.DB.Close() }

// ---- users (mock identity provider) ----

func HashPassword(p string) string {
	sum := sha256.Sum256([]byte("infworks-demo:" + p))
	return hex.EncodeToString(sum[:])
}

func (s *Store) AddUser(ctx context.Context, u domain.User, password string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT OR REPLACE INTO users VALUES (?,?,?,?)`,
		u.Username, u.DisplayName, u.Role, HashPassword(password))
	return err
}

func (s *Store) Authenticate(ctx context.Context, username, password string) (*domain.User, error) {
	u, hash, err := s.user(ctx, username)
	if err != nil {
		return nil, err
	}
	if u == nil || subtle.ConstantTimeCompare([]byte(hash), []byte(HashPassword(password))) != 1 {
		return nil, nil
	}
	return u, nil
}

func (s *Store) User(ctx context.Context, username string) (*domain.User, error) {
	u, _, err := s.user(ctx, username)
	return u, err
}

func (s *Store) user(ctx context.Context, username string) (*domain.User, string, error) {
	var u domain.User
	var hash string
	err := s.DB.QueryRowContext(ctx, `SELECT username, display_name, role, password_hash FROM users WHERE username = ?`, username).
		Scan(&u.Username, &u.DisplayName, &u.Role, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	return &u, hash, nil
}

// ---- works ----

const workCols = `w.id, w.title, w.year, w.type, w.status, w.advisor, w.abstract, w.shelf_location,
	w.external_url, w.pdf_file, w.pdf_name, w.contact_info, w.contact_public, w.version,
	w.created_by, w.created_at, w.updated_at`

type scanner interface{ Scan(...any) error }

func scanWork(r scanner) (domain.Work, error) {
	var w domain.Work
	var created, updated string
	err := r.Scan(&w.ID, &w.Title, &w.Year, &w.Type, &w.Status, &w.Advisor, &w.Abstract, &w.ShelfLocation,
		&w.ExternalURL, &w.PDFFile, &w.PDFName, &w.ContactInfo, &w.ContactPublic, &w.Version,
		&w.CreatedBy, &created, &updated)
	w.CreatedAt = parseTime(created)
	w.UpdatedAt = parseTime(updated)
	return w, err
}

func (s *Store) queryWorks(ctx context.Context, query string, args ...any) ([]domain.Work, error) {
	rows, err := s.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var works []domain.Work
	for rows.Next() {
		w, err := scanWork(rows)
		if err != nil {
			return nil, err
		}
		works = append(works, w)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return works, s.loadTerms(ctx, works)
}

// loadTerms fills authors, keywords and areas for the given works in one query.
func (s *Store) loadTerms(ctx context.Context, works []domain.Work) error {
	if len(works) == 0 {
		return nil
	}
	idx := map[int64]int{}
	ids := make([]any, len(works))
	for i, w := range works {
		idx[w.ID] = i
		ids[i] = w.ID
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT work_id, kind, value FROM work_terms
		WHERE work_id IN (`+placeholders(len(ids))+`) ORDER BY work_id, kind, position`, ids...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var kind, value string
		if err := rows.Scan(&id, &kind, &value); err != nil {
			return err
		}
		w := &works[idx[id]]
		switch kind {
		case "author":
			w.Authors = append(w.Authors, value)
		case "keyword":
			w.Keywords = append(w.Keywords, value)
		case "area":
			w.Areas = append(w.Areas, value)
		}
	}
	return rows.Err()
}

func (s *Store) GetWork(ctx context.Context, id int64) (domain.Work, error) {
	works, err := s.queryWorks(ctx, `SELECT `+workCols+` FROM works w WHERE w.id = ?`, id)
	if err != nil {
		return domain.Work{}, err
	}
	if len(works) == 0 {
		return domain.Work{}, app.ErrNotFound
	}
	return works[0], nil
}

func (s *Store) CreateWork(ctx context.Context, w domain.Work, audit domain.AuditEntry) (int64, error) {
	var id int64
	err := s.tx(ctx, func(tx *sql.Tx) error {
		now := fmtTime(audit.At)
		res, err := tx.ExecContext(ctx, `INSERT INTO works (title, year, type, status, advisor, abstract,
			shelf_location, external_url, pdf_file, pdf_name, contact_info, contact_public, version,
			created_by, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,1,?,?,?)`,
			w.Title, w.Year, w.Type, w.Status, w.Advisor, w.Abstract, w.ShelfLocation, w.ExternalURL,
			w.PDFFile, w.PDFName, w.ContactInfo, w.ContactPublic, w.CreatedBy, now, now)
		if err != nil {
			return err
		}
		if id, err = res.LastInsertId(); err != nil {
			return err
		}
		w.ID, audit.WorkID = id, id
		if err := writeTerms(ctx, tx, w); err != nil {
			return err
		}
		return writeAudit(ctx, tx, audit)
	})
	return id, err
}

func (s *Store) UpdateWork(ctx context.Context, w domain.Work, expectStatus domain.Status, audit domain.AuditEntry) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE works SET title=?, year=?, type=?, status=?, advisor=?,
			abstract=?, shelf_location=?, external_url=?, pdf_file=?, pdf_name=?, contact_info=?,
			contact_public=?, version=version+1, updated_at=?
			WHERE id=? AND status=? AND version=?`,
			w.Title, w.Year, w.Type, w.Status, w.Advisor, w.Abstract, w.ShelfLocation, w.ExternalURL,
			w.PDFFile, w.PDFName, w.ContactInfo, w.ContactPublic, fmtTime(audit.At),
			w.ID, expectStatus, w.Version)
		if err != nil {
			return err
		}
		if err := oneRow(res); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM work_terms WHERE work_id = ?`, w.ID); err != nil {
			return err
		}
		if err := writeTerms(ctx, tx, w); err != nil {
			return err
		}
		audit.WorkID = w.ID
		return writeAudit(ctx, tx, audit)
	})
}

func (s *Store) NonDraftWorksByYear(ctx context.Context, year int, excludeID int64) ([]domain.Work, error) {
	return s.queryWorks(ctx, `SELECT `+workCols+` FROM works w
		WHERE w.year = ? AND w.id <> ? AND w.status <> 'DRAFT' ORDER BY w.id`, year, excludeID)
}

func (s *Store) SearchPublished(ctx context.Context, q app.SearchQuery) ([]domain.Work, int, error) {
	where := []string{`w.status = 'PUBLISHED'`}
	var args []any
	like := func(v string) string {
		v = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(domain.Normalize(v))
		return "%" + v + "%"
	}
	term := func(kind string) string {
		return `EXISTS (SELECT 1 FROM work_terms t WHERE t.work_id = w.id AND t.kind = '` + kind +
			`' AND norm(t.value) LIKE ? ESCAPE '\')`
	}
	if q.Q != "" {
		where = append(where, `(norm(w.title) LIKE ? ESCAPE '\' OR norm(w.abstract) LIKE ? ESCAPE '\' OR `+term("keyword")+`)`)
		args = append(args, like(q.Q), like(q.Q), like(q.Q))
	}
	if q.Author != "" {
		where = append(where, term("author"))
		args = append(args, like(q.Author))
	}
	if q.Advisor != "" {
		where = append(where, `norm(w.advisor) LIKE ? ESCAPE '\'`)
		args = append(args, like(q.Advisor))
	}
	if q.Area != "" {
		where = append(where, term("area"))
		args = append(args, like(q.Area))
	}
	if q.Year != 0 {
		where = append(where, `w.year = ?`)
		args = append(args, q.Year)
	}
	if q.Type != "" {
		where = append(where, `w.type = ?`)
		args = append(args, q.Type)
	}
	cond := strings.Join(where, " AND ")
	var total int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM works w WHERE `+cond, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	// Stable ordering (FR-S-10): newest first, then title, then ID.
	works, err := s.queryWorks(ctx, `SELECT `+workCols+` FROM works w WHERE `+cond+`
		ORDER BY w.year DESC, norm(w.title) ASC, w.id ASC LIMIT ? OFFSET ?`,
		append(args, q.Limit, q.Offset)...)
	return works, total, err
}

func (s *Store) PublishedSharingKeywords(ctx context.Context, keywords []string, excludeID int64) ([]domain.Work, error) {
	if len(keywords) == 0 {
		return nil, nil
	}
	args := []any{excludeID}
	for _, k := range keywords {
		args = append(args, domain.Normalize(k))
	}
	return s.queryWorks(ctx, `SELECT `+workCols+` FROM works w WHERE w.status = 'PUBLISHED' AND w.id <> ?
		AND w.id IN (SELECT work_id FROM work_terms WHERE kind = 'keyword' AND norm(value) IN (`+placeholders(len(keywords))+`))
		ORDER BY w.id`, args...)
}

func (s *Store) PublishedAreas(ctx context.Context) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT DISTINCT t.value FROM work_terms t JOIN works w ON w.id = t.work_id
		WHERE t.kind = 'area' AND w.status = 'PUBLISHED' ORDER BY t.value COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) ListWorks(ctx context.Context, status domain.Status) ([]domain.Work, error) {
	return s.queryWorks(ctx, `SELECT `+workCols+` FROM works w WHERE (? = '' OR w.status = ?)
		ORDER BY w.updated_at DESC, w.id DESC`, status, status)
}

// ---- moderation ----

func (s *Store) RecordDecision(ctx context.Context, version int, d domain.ReviewDecision, to domain.Status) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE works SET status = ?, version = version + 1, updated_at = ?
			WHERE id = ? AND status = 'UNDER_REVIEW' AND version = ?`, to, fmtTime(d.DecidedAt), d.WorkID, version)
		if err != nil {
			return err
		}
		if err := oneRow(res); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO review_decisions (work_id, decision, reason, moderator, decided_at)
			VALUES (?,?,?,?,?)`, d.WorkID, d.Decision, d.Reason, d.Moderator, fmtTime(d.DecidedAt)); err != nil {
			return err
		}
		detail := "approved and published"
		if d.Decision == domain.DecisionRejected {
			detail = "rejected: " + d.Reason
		}
		return writeAudit(ctx, tx, domain.AuditEntry{WorkID: d.WorkID, Actor: d.Moderator,
			Action: string(d.Decision), Detail: detail, At: d.DecidedAt})
	})
}

const decisionCols = `id, work_id, decision, reason, moderator, decided_at`

func scanDecisions(rows *sql.Rows) ([]domain.ReviewDecision, error) {
	defer rows.Close()
	var out []domain.ReviewDecision
	for rows.Next() {
		var d domain.ReviewDecision
		var at string
		if err := rows.Scan(&d.ID, &d.WorkID, &d.Decision, &d.Reason, &d.Moderator, &at); err != nil {
			return nil, err
		}
		d.DecidedAt = parseTime(at)
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) Decisions(ctx context.Context, workID int64) ([]domain.ReviewDecision, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+decisionCols+` FROM review_decisions WHERE work_id = ? ORDER BY id DESC`, workID)
	if err != nil {
		return nil, err
	}
	return scanDecisions(rows)
}

func (s *Store) LatestDecisions(ctx context.Context) (map[int64]domain.ReviewDecision, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+decisionCols+` FROM review_decisions d
		WHERE d.id = (SELECT MAX(id) FROM review_decisions WHERE work_id = d.work_id)`)
	if err != nil {
		return nil, err
	}
	list, err := scanDecisions(rows)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]domain.ReviewDecision, len(list))
	for _, d := range list {
		out[d.WorkID] = d
	}
	return out, nil
}

func (s *Store) Audit(ctx context.Context, workID int64) ([]domain.AuditEntry, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, work_id, actor, action, detail, at FROM audit_entries
		WHERE work_id = ? ORDER BY id DESC`, workID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.AuditEntry
	for rows.Next() {
		var a domain.AuditEntry
		var at string
		if err := rows.Scan(&a.ID, &a.WorkID, &a.Actor, &a.Action, &a.Detail, &at); err != nil {
			return nil, err
		}
		a.At = parseTime(at)
		out = append(out, a)
	}
	return out, rows.Err()
}

// ---- helpers ----

func (s *Store) tx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func oneRow(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return app.ErrConflict
	}
	return nil
}

func writeTerms(ctx context.Context, tx *sql.Tx, w domain.Work) error {
	for kind, values := range map[string][]string{"author": w.Authors, "keyword": w.Keywords, "area": w.Areas} {
		for i, v := range values {
			if _, err := tx.ExecContext(ctx, `INSERT INTO work_terms VALUES (?,?,?,?)`, w.ID, kind, i, v); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeAudit(ctx context.Context, tx *sql.Tx, a domain.AuditEntry) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO audit_entries (work_id, actor, action, detail, at) VALUES (?,?,?,?,?)`,
		a.WorkID, a.Actor, a.Action, a.Detail, fmtTime(a.At))
	return err
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

func fmtTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}
