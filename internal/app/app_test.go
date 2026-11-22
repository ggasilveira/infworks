package app_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"infworks/internal/app"
	"infworks/internal/domain"
	"infworks/internal/store"
)

var (
	ctx         = context.Background()
	fixedNow    = func() time.Time { return time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC) }
	library     = &domain.User{Username: "library", Role: domain.RoleLibraryWorker}
	coordinator = &domain.User{Username: "coordinator", Role: domain.RoleCoordinator}
	researcher  = &domain.User{Username: "researcher", Role: domain.RoleResearcher}
)

type env struct {
	repo   *store.Store
	docs   *store.FileStore
	search *app.Search
	cat    *app.Cataloguing
	mod    *app.Moderation
}

func newEnv(t *testing.T) *env {
	t.Helper()
	dir := t.TempDir()
	repo, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { repo.Close() })
	docs, err := store.NewFileStore(filepath.Join(dir, "docs"))
	if err != nil {
		t.Fatal(err)
	}
	return &env{repo, docs,
		&app.Search{Repo: repo, Now: fixedNow},
		&app.Cataloguing{Repo: repo, Docs: docs, Now: fixedNow},
		&app.Moderation{Repo: repo, Now: fixedNow}}
}

// publish inserts a published work directly.
func (e *env) publish(t *testing.T, w domain.Work) int64 {
	t.Helper()
	w.Status = domain.StatusPublished
	if w.Type == "" {
		w.Type = domain.TypeTCC
	}
	id, err := e.repo.CreateWork(ctx, w, domain.AuditEntry{Actor: "test", Action: "SEED", At: fixedNow()})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func complete(title string) app.WorkInput {
	return app.WorkInput{Title: title, Year: "2025", Type: "DISSERTATION", Authors: "Ana Souza\nBruno Lima",
		Advisor: "Helena Duarte", Areas: "Artificial Intelligence", Keywords: "machine learning, graphs",
		Abstract: "An abstract.", ExternalURL: "https://repository.example.org/handle/1"}
}

func pdfUpload() *app.Upload { return &app.Upload{Name: "work.pdf", Data: store.SamplePDF("t")} }

// submitAndGet submits a new complete record and returns the stored work.
func (e *env) submit(t *testing.T, in app.WorkInput) domain.Work {
	t.Helper()
	id, err := e.cat.Save(ctx, library, 0, in, nil, true)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	w, _ := e.repo.GetWork(ctx, id)
	return w
}

// ---------- F1: search ----------

// TS-01 / AC-01a
func TestTS01_ValidSearchShowsCardFields(t *testing.T) {
	e := newEnv(t)
	e.publish(t, domain.Work{Title: "Deep Nets", Year: 2024, Authors: []string{"Marco", "Otto"}, Advisor: "Ana",
		Keywords: []string{"Machine Learning"}})
	res, err := e.search.Search(ctx, app.SearchParams{Q: "machine learning"})
	if err != nil || res.Total != 1 {
		t.Fatalf("got %d results, err %v", res.Total, err)
	}
	w := res.Works[0]
	if w.Title != "Deep Nets" || w.FirstAuthor() != "Marco" || w.Year != 2024 || w.Type != domain.TypeTCC || w.Advisor != "Ana" {
		t.Errorf("card fields wrong: %+v", w)
	}
}

// TS-02 / AC-01b: filters combine with AND.
func TestTS02_FiltersIntersect(t *testing.T) {
	e := newEnv(t)
	e.publish(t, domain.Work{Title: "A", Year: 2024, Areas: []string{"Databases"}})
	e.publish(t, domain.Work{Title: "B", Year: 2023, Areas: []string{"Databases"}})
	e.publish(t, domain.Work{Title: "C", Year: 2024, Areas: []string{"Networks"}})
	res, err := e.search.Search(ctx, app.SearchParams{Area: "databases", Year: "2024"})
	if err != nil || res.Total != 1 || res.Works[0].Title != "A" {
		t.Fatalf("want only A, got %+v (err %v)", res.Works, err)
	}
}

// TS-03 / AC-01c
func TestTS03_EmptyResult(t *testing.T) {
	e := newEnv(t)
	e.publish(t, domain.Work{Title: "Something"})
	res, err := e.search.Search(ctx, app.SearchParams{Q: "zzqx unmatched phrase"})
	if err != nil || res.Total != 0 || len(res.Works) != 0 {
		t.Fatalf("want empty, got %d (err %v)", res.Total, err)
	}
}

// TS-04..TS-06 at service level: 201 characters is rejected and no search runs.
func TestTS06_OverlongQueryRejected(t *testing.T) {
	e := newEnv(t)
	long := fmt.Sprintf("%0201d", 0)
	_, err := e.search.Search(ctx, app.SearchParams{Advisor: long})
	var v *app.ValidationError
	if !errors.As(err, &v) || v.Fields["advisor"] == "" {
		t.Fatalf("want advisor validation error, got %v", err)
	}
	if _, err := e.search.Search(ctx, app.SearchParams{Q: long[:200]}); err != nil {
		t.Fatalf("200 chars should be accepted: %v", err)
	}
}

// TS-07: empty text with a filter is accepted.
func TestTS07_FilterOnly(t *testing.T) {
	e := newEnv(t)
	e.publish(t, domain.Work{Title: "A", Areas: []string{"HCI"}})
	res, err := e.search.Search(ctx, app.SearchParams{Area: "HCI"})
	if err != nil || res.Total != 1 {
		t.Fatalf("got %d, err %v", res.Total, err)
	}
}

// TS-08 / AC-01e / BR-02: only PUBLISHED records are searchable.
func TestTS08_OnlyPublishedVisible(t *testing.T) {
	e := newEnv(t)
	e.publish(t, domain.Work{Title: "Visible graphs"})
	if _, err := e.cat.Save(ctx, library, 0, app.WorkInput{Title: "Draft graphs"}, nil, false); err != nil {
		t.Fatal(err)
	}
	e.submit(t, complete("Pending graphs"))
	rejected := e.submit(t, complete("Rejected graphs"))
	if err := e.mod.Decide(ctx, coordinator, rejected.ID, rejected.Version, false, "bad"); err != nil {
		t.Fatal(err)
	}
	res, _ := e.search.Search(ctx, app.SearchParams{Q: "graphs"})
	if res.Total != 1 || res.Works[0].Title != "Visible graphs" {
		t.Fatalf("want only the published record, got %+v", res.Works)
	}
	if _, _, err := e.search.Details(ctx, rejected.ID); !errors.Is(err, app.ErrNotFound) {
		t.Errorf("non-published details must be hidden, got %v", err)
	}
}

// TS-09 / AC-02a
func TestTS09_DetailsShowLink(t *testing.T) {
	e := newEnv(t)
	id := e.publish(t, domain.Work{Title: "Linked", ExternalURL: "https://example.org/w"})
	w, _, err := e.search.Details(ctx, id)
	if err != nil || w.ExternalURL != "https://example.org/w" {
		t.Fatalf("details: %+v, %v", w, err)
	}
}

// TS-10 / AC-02b: similar works are published only, exclude current, ranked by overlap, max 5.
func TestTS10_SimilarWorks(t *testing.T) {
	e := newEnv(t)
	cur := e.publish(t, domain.Work{Title: "Current", Keywords: []string{"graphs", "search", "ai"}})
	two := e.publish(t, domain.Work{Title: "Two overlaps", Year: 2020, Keywords: []string{"Graphs", "AI"}})
	for i := 0; i < 5; i++ {
		e.publish(t, domain.Work{Title: fmt.Sprintf("One %d", i), Year: 2021, Keywords: []string{"search"}})
	}
	e.publish(t, domain.Work{Title: "Unrelated", Keywords: []string{"networks"}})
	pending := complete("Pending related")
	pending.Keywords = "graphs, search, ai"
	e.submit(t, pending)

	_, similar, err := e.search.Details(ctx, cur)
	if err != nil {
		t.Fatal(err)
	}
	if len(similar) != 5 || similar[0].ID != two {
		t.Fatalf("want 5 results led by the two-overlap work, got %d: %+v", len(similar), similar)
	}
	for _, s := range similar {
		if s.ID == cur || s.Status != domain.StatusPublished {
			t.Errorf("unexpected similar work %+v", s)
		}
	}
}

// TS-11: 23 matches give pages of 10, 10, 3 with stable order.
func TestTS11_Pagination(t *testing.T) {
	e := newEnv(t)
	for i := 0; i < 23; i++ {
		e.publish(t, domain.Work{Title: "Same title", Year: 2020, Keywords: []string{"paging"}})
	}
	seen := map[int64]bool{}
	for page, want := range map[int]int{1: 10, 2: 10, 3: 3} {
		res, err := e.search.Search(ctx, app.SearchParams{Q: "paging", Page: page})
		if err != nil || len(res.Works) != want || res.Total != 23 || res.Pages != 3 {
			t.Fatalf("page %d: %d works, total %d, err %v", page, len(res.Works), res.Total, err)
		}
		again, _ := e.search.Search(ctx, app.SearchParams{Q: "paging", Page: page})
		for i, w := range res.Works {
			if again.Works[i].ID != w.ID {
				t.Fatalf("page %d not stable", page)
			}
			if seen[w.ID] {
				t.Fatalf("work %d appears on two pages", w.ID)
			}
			seen[w.ID] = true
		}
	}
}

// TS-12 / NFR-06: database failure yields ErrUnavailable and no results.
func TestTS12_DatabaseFailure(t *testing.T) {
	e := newEnv(t)
	e.publish(t, domain.Work{Title: "A"})
	e.repo.Close()
	res, err := e.search.Search(ctx, app.SearchParams{Q: "A"})
	if !errors.Is(err, app.ErrUnavailable) || len(res.Works) != 0 {
		t.Fatalf("want ErrUnavailable and no works, got %v, %d", err, len(res.Works))
	}
}

// ---------- F2: registration ----------

// TW-01 / AC-03a
func TestTW01_DraftWithTitleOnly(t *testing.T) {
	e := newEnv(t)
	id, err := e.cat.Save(ctx, library, 0, app.WorkInput{Title: "Only title"}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	w, _ := e.repo.GetWork(ctx, id)
	if w.Status != domain.StatusDraft {
		t.Fatalf("status %s", w.Status)
	}
	if res, _ := e.search.Search(ctx, app.SearchParams{Q: "Only title"}); res.Total != 0 {
		t.Error("draft visible publicly")
	}
}

// TW-02 / AC-03b, TW-10, TW-11, TW-12: valid sources lead to UNDER_REVIEW and the queue.
func TestTW02_TW10_TW11_TW12_ValidSubmission(t *testing.T) {
	cases := map[string]struct {
		url string
		pdf bool
	}{"TW-10 PDF only": {"", true}, "TW-11 URL only": {"https://example.org/h/1", false}, "TW-12 both": {"https://example.org/h/2", true}}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			in := complete("Valid " + name)
			in.ExternalURL = c.url
			var up *app.Upload
			if c.pdf {
				up = pdfUpload()
			}
			id, err := e.cat.Save(ctx, library, 0, in, up, true)
			if err != nil {
				t.Fatal(err)
			}
			w, _ := e.repo.GetWork(ctx, id)
			if w.Status != domain.StatusUnderReview || w.HasPDF() != c.pdf {
				t.Fatalf("got %+v", w)
			}
			queue, _ := e.mod.Queue(ctx, coordinator)
			if len(queue) != 1 || queue[0].ID != id {
				t.Fatalf("not in queue: %+v", queue)
			}
		})
	}
}

// TW-03..TW-09, TW-13: incomplete submissions fail with field errors but can still be saved as drafts.
func TestTW03to09_MissingFields(t *testing.T) {
	mutations := map[string]func(*app.WorkInput){
		"title": func(in *app.WorkInput) { in.Title = " " }, "year": func(in *app.WorkInput) { in.Year = "" },
		"authors": func(in *app.WorkInput) { in.Authors = "" }, "advisor": func(in *app.WorkInput) { in.Advisor = "" },
		"areas": func(in *app.WorkInput) { in.Areas = "" }, "keywords": func(in *app.WorkInput) { in.Keywords = " , " },
		"source": func(in *app.WorkInput) { in.ExternalURL = "" },
	}
	for field, mutate := range mutations {
		t.Run(field, func(t *testing.T) {
			e := newEnv(t)
			in := complete("Missing " + field)
			mutate(&in)
			_, err := e.cat.Save(ctx, library, 0, in, nil, true)
			var v *app.ValidationError
			if !errors.As(err, &v) || v.Fields[field] == "" {
				t.Fatalf("want error on %s, got %v", field, err)
			}
			if works, _ := e.repo.ListWorks(ctx, ""); len(works) != 0 {
				t.Fatal("failed submission stored a record")
			}
			if _, err := e.cat.Save(ctx, library, 0, in, nil, false); err != nil {
				t.Fatalf("draft should still save: %v", err)
			}
		})
	}
}

// TW-05: invalid year text is rejected.
func TestTW05_InvalidYear(t *testing.T) {
	e := newEnv(t)
	in := complete("Bad year")
	in.Year = "abcd"
	_, err := e.cat.Save(ctx, library, 0, in, nil, true)
	var v *app.ValidationError
	if !errors.As(err, &v) || v.Fields["year"] == "" {
		t.Fatalf("want year error, got %v", err)
	}
}

// TW-14 / AC-03d, TW-15: unsupported file and malformed URL are rejected.
func TestTW14_TW15_BadSources(t *testing.T) {
	e := newEnv(t)
	in := complete("Bad file")
	in.ExternalURL = ""
	_, err := e.cat.Save(ctx, library, 0, in, &app.Upload{Name: "work.docx", Data: []byte("PK\x03\x04docx")}, true)
	var v *app.ValidationError
	if !errors.As(err, &v) || v.Fields["pdf"] == "" {
		t.Fatalf("TW-14: want pdf error, got %v", err)
	}
	in.ExternalURL = "htp:/broken url"
	_, err = e.cat.Save(ctx, library, 0, in, nil, true)
	if !errors.As(err, &v) || v.Fields["external_url"] == "" {
		t.Fatalf("TW-15: want url error, got %v", err)
	}
}

// TW-16 / AC-03c, TW-17, TW-18: duplicate rule.
func TestTW16to18_Duplicates(t *testing.T) {
	e := newEnv(t)
	first := e.submit(t, complete("Graph Search for Campus Routes"))

	in := complete("Graph Search for Campus Routes")
	_, err := e.cat.Save(ctx, library, 0, in, nil, true)
	var d *app.DuplicateError
	if !errors.As(err, &d) || d.ExistingID != first.ID {
		t.Fatalf("TW-16: want duplicate of %d, got %v", first.ID, err)
	}
	in.Title = "  graph SEARCH for   campus routes "
	in.Authors = "ana souza"
	if _, err = e.cat.Save(ctx, library, 0, in, nil, true); !errors.As(err, &d) {
		t.Fatalf("TW-17: normalized variant not blocked: %v", err)
	}
	in = complete("Graph Search for Campus Routes")
	in.Year = "2024"
	if _, err := e.cat.Save(ctx, library, 0, in, nil, true); err != nil {
		t.Fatalf("TW-18: different year must pass: %v", err)
	}
	in = complete("Graph Search for Campus Routes")
	in.Authors = "Someone Else"
	if _, err := e.cat.Save(ctx, library, 0, in, nil, true); err != nil {
		t.Fatalf("TW-18: different first author must pass: %v", err)
	}
}

// TW-19 / AC-03e / AC-04c: roles enforced in the services.
func TestTW19_Authorization(t *testing.T) {
	e := newEnv(t)
	for _, u := range []*domain.User{nil, researcher, coordinator} {
		_, err := e.cat.Save(ctx, u, 0, complete("x"), nil, false)
		if !errors.Is(err, app.ErrForbidden) && !errors.Is(err, app.ErrUnauthenticated) {
			t.Errorf("%v could write: %v", u, err)
		}
	}
	w := e.submit(t, complete("Pending"))
	for _, u := range []*domain.User{nil, researcher, library} {
		if err := e.mod.Decide(ctx, u, w.ID, w.Version, true, ""); err == nil {
			t.Errorf("%v could moderate", u)
		}
	}
	if works, _ := e.repo.ListWorks(ctx, ""); len(works) != 1 || works[0].Status != domain.StatusUnderReview {
		t.Error("unauthorized calls changed data")
	}
}

// ---------- F2 support: moderation ----------

// TW-20 / AC-04a
func TestTW20_Approve(t *testing.T) {
	e := newEnv(t)
	w := e.submit(t, complete("To approve"))
	if err := e.mod.Decide(ctx, coordinator, w.ID, w.Version, true, ""); err != nil {
		t.Fatal(err)
	}
	if res, _ := e.search.Search(ctx, app.SearchParams{Q: "To approve"}); res.Total != 1 {
		t.Fatal("approved work not discoverable")
	}
	ds, _ := e.repo.Decisions(ctx, w.ID)
	if len(ds) != 1 || ds[0].Moderator != "coordinator" || ds[0].DecidedAt.IsZero() {
		t.Fatalf("decision not recorded: %+v", ds)
	}
}

// TW-21, TW-22 / AC-04b
func TestTW21_TW22_Reject(t *testing.T) {
	e := newEnv(t)
	w := e.submit(t, complete("To reject"))
	err := e.mod.Decide(ctx, coordinator, w.ID, w.Version, false, "   ")
	var v *app.ValidationError
	if !errors.As(err, &v) {
		t.Fatalf("TW-21: want validation error, got %v", err)
	}
	if got, _ := e.repo.GetWork(ctx, w.ID); got.Status != domain.StatusUnderReview {
		t.Fatal("TW-21: status changed")
	}
	if err := e.mod.Decide(ctx, coordinator, w.ID, w.Version, false, "Wrong advisor"); err != nil {
		t.Fatal(err)
	}
	got, _ := e.repo.GetWork(ctx, w.ID)
	ds, _ := e.repo.Decisions(ctx, w.ID)
	if got.Status != domain.StatusRejected || ds[0].Reason != "Wrong advisor" {
		t.Fatalf("TW-22: %s %+v", got.Status, ds)
	}
}

// TW-23: corrected rejected record is resubmitted; history is retained.
func TestTW23_Resubmit(t *testing.T) {
	e := newEnv(t)
	w := e.submit(t, complete("Fix me"))
	e.mod.Decide(ctx, coordinator, w.ID, w.Version, false, "Fix the abstract")
	w, _ = e.repo.GetWork(ctx, w.ID)
	in := complete("Fix me")
	in.Abstract, in.Version = "Corrected abstract.", w.Version
	if _, err := e.cat.Save(ctx, library, w.ID, in, nil, true); err != nil {
		t.Fatal(err)
	}
	got, _ := e.repo.GetWork(ctx, w.ID)
	ds, _ := e.repo.Decisions(ctx, w.ID)
	if got.Status != domain.StatusUnderReview || len(ds) != 1 || ds[0].Decision != domain.DecisionRejected {
		t.Fatalf("got %s, decisions %+v", got.Status, ds)
	}
}

// TW-24: DRAFT->PUBLISHED and REJECTED->PUBLISHED are impossible through the services.
func TestTW24_InvalidTransitions(t *testing.T) {
	e := newEnv(t)
	draftID, _ := e.cat.Save(ctx, library, 0, complete("Draft"), nil, false)
	d, _ := e.repo.GetWork(ctx, draftID)
	if err := e.mod.Decide(ctx, coordinator, draftID, d.Version, true, ""); !errors.Is(err, app.ErrConflict) {
		t.Errorf("draft approval: %v", err)
	}
	r := e.submit(t, complete("Rejected"))
	e.mod.Decide(ctx, coordinator, r.ID, r.Version, false, "no")
	r, _ = e.repo.GetWork(ctx, r.ID)
	if err := e.mod.Decide(ctx, coordinator, r.ID, r.Version, true, ""); !errors.Is(err, app.ErrConflict) {
		t.Errorf("rejected approval: %v", err)
	}
	for id, want := range map[int64]domain.Status{draftID: domain.StatusDraft, r.ID: domain.StatusRejected} {
		if got, _ := e.repo.GetWork(ctx, id); got.Status != want {
			t.Errorf("work %d status changed to %s", id, got.Status)
		}
	}
	// Editing a record under review is also refused.
	p := e.submit(t, complete("Pending"))
	in := complete("Pending")
	in.Version = p.Version
	var te *app.TransitionError
	if _, err := e.cat.Save(ctx, library, p.ID, in, nil, false); !errors.As(err, &te) {
		t.Errorf("edit under review: %v", err)
	}
}

// TW-25 / AC-04c: two coordinators decide on the same version; the second is blocked.
func TestTW25_StaleDecision(t *testing.T) {
	e := newEnv(t)
	w := e.submit(t, complete("Contested"))
	second := &domain.User{Username: "coordinator2", Role: domain.RoleCoordinator}
	if err := e.mod.Decide(ctx, coordinator, w.ID, w.Version, true, ""); err != nil {
		t.Fatal(err)
	}
	if err := e.mod.Decide(ctx, second, w.ID, w.Version, false, "late"); !errors.Is(err, app.ErrConflict) {
		t.Fatalf("want conflict, got %v", err)
	}
	ds, _ := e.repo.Decisions(ctx, w.ID)
	if got, _ := e.repo.GetWork(ctx, w.ID); got.Status != domain.StatusPublished || len(ds) != 1 {
		t.Fatal("second decision overwrote the first")
	}
}

// failingRepo fails the final persistence step of a submission.
type failingRepo struct{ app.Repository }

func (failingRepo) CreateWork(context.Context, domain.Work, domain.AuditEntry) (int64, error) {
	return 0, errors.New("disk full")
}

// TW-26 / NFR-06: a failed submission leaves no queue entry and no orphan file.
func TestTW26_AtomicSubmission(t *testing.T) {
	e := newEnv(t)
	cat := &app.Cataloguing{Repo: failingRepo{e.repo}, Docs: e.docs, Now: fixedNow}
	_, err := cat.Save(ctx, library, 0, complete("Will fail"), pdfUpload(), true)
	if !errors.Is(err, app.ErrUnavailable) {
		t.Fatalf("want ErrUnavailable, got %v", err)
	}
	if q, _ := e.mod.Queue(ctx, coordinator); len(q) != 0 {
		t.Fatal("failed submission appeared in queue")
	}
	if files, _ := os.ReadDir(e.docs.Dir); len(files) != 0 {
		t.Fatalf("orphan PDF left behind: %v", files)
	}
}

// TW-27 / FR-W-04: editing a published record keeps it published and audits the change.
func TestTW27_EditPublishedIsAudited(t *testing.T) {
	e := newEnv(t)
	w := e.submit(t, complete("Published one"))
	e.mod.Decide(ctx, coordinator, w.ID, w.Version, true, "")
	w, _ = e.repo.GetWork(ctx, w.ID)
	in := complete("Published one (revised)")
	in.Version = w.Version
	if _, err := e.cat.Save(ctx, library, w.ID, in, nil, false); err != nil {
		t.Fatal(err)
	}
	got, _ := e.repo.GetWork(ctx, w.ID)
	audit, _ := e.repo.Audit(ctx, w.ID)
	if got.Status != domain.StatusPublished || got.Title != "Published one (revised)" {
		t.Fatalf("got %+v", got)
	}
	if audit[0].Action != "EDIT_PUBLISHED" || audit[0].Actor != "library" || audit[0].Detail != "changed: title" {
		t.Fatalf("audit: %+v", audit[0])
	}
	// A published edit may not drop mandatory metadata.
	in.Advisor, in.Version = "", got.Version
	var v *app.ValidationError
	if _, err := e.cat.Save(ctx, library, w.ID, in, nil, false); !errors.As(err, &v) {
		t.Fatalf("incomplete published edit accepted: %v", err)
	}
}

// Optimistic locking: an edit based on an old version is refused.
func TestStaleEditRefused(t *testing.T) {
	e := newEnv(t)
	id, _ := e.cat.Save(ctx, library, 0, app.WorkInput{Title: "v1", Version: 0}, nil, false)
	if _, err := e.cat.Save(ctx, library, id, app.WorkInput{Title: "v2", Version: 1}, nil, false); err != nil {
		t.Fatal(err)
	}
	if _, err := e.cat.Save(ctx, library, id, app.WorkInput{Title: "v2b", Version: 1}, nil, false); !errors.Is(err, app.ErrConflict) {
		t.Fatalf("want conflict, got %v", err)
	}
}
