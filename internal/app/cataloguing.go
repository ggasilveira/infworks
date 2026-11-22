package app

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"infworks/internal/domain"
)

// WorkInput is the raw registration form (UC-02).
type WorkInput struct {
	Title, Year, Type, Authors, Advisor, Areas, Keywords string
	Abstract, ShelfLocation, ExternalURL, ContactInfo    string
	ContactPublic, RemovePDF                             bool
	Version                                              int // version the form was loaded from
}

// Upload is an attached document; nil when no file was sent.
type Upload struct {
	Name string
	Data []byte
}

type Cataloguing struct {
	Repo Repository
	Docs DocumentStore
	Now  func() time.Time
}

// Save creates or updates a record. With submit=false it saves a draft (or an
// audited edit of a published/rejected record); with submit=true it validates
// and moves the record to UNDER_REVIEW atomically. It returns the record ID.
func (c *Cataloguing) Save(ctx context.Context, u *domain.User, id int64, in WorkInput, up *Upload, submit bool) (int64, error) {
	if err := requireRole(u, domain.RoleLibraryWorker); err != nil {
		return 0, err
	}
	var cur domain.Work
	if id != 0 {
		var err error
		if cur, err = c.Repo.GetWork(ctx, id); err != nil {
			return 0, unavailable(err)
		}
		if in.Version != cur.Version {
			return 0, ErrConflict
		}
	}

	target := domain.StatusDraft
	switch {
	case submit:
		target = domain.StatusUnderReview
	case cur.Status == domain.StatusPublished || cur.Status == domain.StatusRejected:
		target = cur.Status
	case cur.Status == domain.StatusUnderReview:
		target = domain.StatusUnderReview // falls through to the transition check and is refused
	}
	if !domain.CanTransition(cur.Status, target, u.Role) {
		return 0, &TransitionError{From: cur.Status, To: target}
	}

	w, errs := c.apply(cur, in)
	if up != nil {
		if msg := domain.CheckPDF(up.Name, up.Data); msg != "" {
			errs["pdf"] = msg
		} else {
			w.PDFFile, w.PDFName = "(pending upload)", up.Name
		}
	}
	needsComplete := target == domain.StatusUnderReview || target == domain.StatusPublished
	if needsComplete {
		for k, v := range domain.ValidateForSubmission(w) {
			if _, ok := errs[k]; !ok {
				errs[k] = v
			}
		}
	}
	if errs.Any() {
		return 0, &ValidationError{errs}
	}
	if needsComplete {
		if dupID, err := c.findDuplicate(ctx, w); err != nil {
			return 0, err
		} else if dupID != 0 {
			return 0, &DuplicateError{ExistingID: dupID}
		}
	}

	if up != nil {
		name, err := c.Docs.Save(up.Data)
		if err != nil {
			return 0, unavailable(err)
		}
		w.PDFFile = name
	}
	w.Status = target
	audit := domain.AuditEntry{Actor: u.Username, Action: auditAction(cur.Status, target, id == 0), Detail: changes(cur, w), At: c.Now()}

	var err error
	if id == 0 {
		w.CreatedBy = u.Username
		id, err = c.Repo.CreateWork(ctx, w, audit)
	} else {
		err = c.Repo.UpdateWork(ctx, w, cur.Status, audit)
	}
	if err != nil {
		// NFR-06: nothing persisted, so drop the file we just stored.
		if up != nil {
			_ = c.Docs.Remove(w.PDFFile)
		}
		return 0, unavailable(err)
	}
	if cur.PDFFile != "" && cur.PDFFile != w.PDFFile {
		_ = c.Docs.Remove(cur.PDFFile)
	}
	return id, nil
}

// apply merges the form into the current record; blank values are allowed for drafts.
func (c *Cataloguing) apply(cur domain.Work, in WorkInput) (domain.Work, domain.FieldErrors) {
	errs := domain.FieldErrors{}
	w := cur
	w.Title = strings.TrimSpace(in.Title)
	w.Type = domain.WorkType(in.Type)
	if in.Type != "" && !w.Type.Valid() {
		errs["type"] = "Choose TCC, dissertation or thesis."
	}
	var msg string
	if w.Year, msg = domain.ParseYear(in.Year, c.Now().Year()); msg != "" {
		errs["year"] = msg
	}
	w.Authors = splitList(in.Authors, "\n")
	w.Advisor = strings.TrimSpace(in.Advisor)
	w.Areas = splitList(in.Areas, ",")
	w.Keywords = splitList(in.Keywords, ",")
	w.Abstract = strings.TrimSpace(in.Abstract)
	w.ShelfLocation = strings.TrimSpace(in.ShelfLocation)
	w.ContactInfo = strings.TrimSpace(in.ContactInfo)
	w.ContactPublic = in.ContactPublic && w.ContactInfo != ""
	w.ExternalURL = strings.TrimSpace(in.ExternalURL)
	if w.ExternalURL != "" {
		if msg := domain.CheckURL(w.ExternalURL); msg != "" {
			errs["external_url"] = msg
		}
	}
	if in.RemovePDF {
		w.PDFFile, w.PDFName = "", ""
	}
	return w, errs
}

func (c *Cataloguing) findDuplicate(ctx context.Context, w domain.Work) (int64, error) {
	others, err := c.Repo.NonDraftWorksByYear(ctx, w.Year, w.ID)
	if err != nil {
		return 0, unavailable(err)
	}
	key := domain.DuplicateKey(w)
	for _, o := range others {
		if domain.DuplicateKey(o) == key {
			return o.ID, nil
		}
	}
	return 0, nil
}

// splitList trims items, drops blanks and removes case-insensitive repeats.
func splitList(s, sep string) []string {
	var out []string
	seen := map[string]bool{}
	for _, part := range strings.Split(s, sep) {
		part = strings.Join(strings.Fields(part), " ")
		if part == "" || seen[domain.Normalize(part)] {
			continue
		}
		seen[domain.Normalize(part)] = true
		out = append(out, part)
	}
	return out
}

func auditAction(from, to domain.Status, created bool) string {
	switch {
	case created && to == domain.StatusDraft:
		return "CREATE_DRAFT"
	case created:
		return "CREATE_AND_SUBMIT"
	case from == domain.StatusRejected && to == domain.StatusUnderReview:
		return "RESUBMIT"
	case to == domain.StatusUnderReview:
		return "SUBMIT"
	case to == domain.StatusPublished:
		return "EDIT_PUBLISHED"
	}
	return "EDIT_" + string(to)
}

// changes lists the fields that differ, for the audit history (FR-W-04).
func changes(a, b domain.Work) string {
	var out []string
	add := func(name string, changed bool) {
		if changed {
			out = append(out, name)
		}
	}
	add("title", a.Title != b.Title)
	add("year", a.Year != b.Year)
	add("type", a.Type != b.Type)
	add("authors", !slices.Equal(a.Authors, b.Authors))
	add("advisor", a.Advisor != b.Advisor)
	add("research areas", !slices.Equal(a.Areas, b.Areas))
	add("keywords", !slices.Equal(a.Keywords, b.Keywords))
	add("abstract", a.Abstract != b.Abstract)
	add("shelf location", a.ShelfLocation != b.ShelfLocation)
	add("external URL", a.ExternalURL != b.ExternalURL)
	add("PDF", a.PDFFile != b.PDFFile)
	add("contact", a.ContactInfo != b.ContactInfo || a.ContactPublic != b.ContactPublic)
	if len(out) == 0 {
		return "no metadata changes"
	}
	return "changed: " + strings.Join(out, ", ")
}

// StaffView is a record with its moderation and audit history.
type StaffView struct {
	Work      domain.Work
	Decisions []domain.ReviewDecision
	Audit     []domain.AuditEntry
}

func (c *Cataloguing) View(ctx context.Context, u *domain.User, id int64) (StaffView, error) {
	if err := requireRole(u, domain.RoleLibraryWorker, domain.RoleCoordinator); err != nil {
		return StaffView{}, err
	}
	w, err := c.Repo.GetWork(ctx, id)
	if err != nil {
		return StaffView{}, unavailable(err)
	}
	d, err := c.Repo.Decisions(ctx, id)
	if err != nil {
		return StaffView{}, unavailable(err)
	}
	a, err := c.Repo.Audit(ctx, id)
	if err != nil {
		return StaffView{}, unavailable(err)
	}
	return StaffView{Work: w, Decisions: d, Audit: a}, nil
}

// DashboardRow pairs a record with its latest moderation decision (FR-W-10).
type DashboardRow struct {
	Work     domain.Work
	Decision *domain.ReviewDecision
}

func (c *Cataloguing) Dashboard(ctx context.Context, u *domain.User, status domain.Status) ([]DashboardRow, error) {
	if err := requireRole(u, domain.RoleLibraryWorker, domain.RoleCoordinator); err != nil {
		return nil, err
	}
	works, err := c.Repo.ListWorks(ctx, status)
	if err != nil {
		return nil, unavailable(err)
	}
	latest, err := c.Repo.LatestDecisions(ctx)
	if err != nil {
		return nil, unavailable(err)
	}
	rows := make([]DashboardRow, len(works))
	for i, w := range works {
		rows[i].Work = w
		if d, ok := latest[w.ID]; ok {
			rows[i].Decision = &d
		}
	}
	return rows, nil
}

// IsUserError reports whether err is a caller mistake rather than an outage.
func IsUserError(err error) bool {
	var v *ValidationError
	var d *DuplicateError
	var t *TransitionError
	return errors.As(err, &v) || errors.As(err, &d) || errors.As(err, &t)
}
