package web

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"infworks/internal/app"
	"infworks/internal/domain"
)

// ---- F1: public search and details ----

type searchData struct {
	Params   app.SearchParams
	Areas    []string
	Errors   domain.FieldErrors
	Searched bool
	Result   app.SearchResult
	Pages    []pageLink
	Prev     string
	Next     string
	Failure  string
	MaxLen   int
}

type pageLink struct {
	N       int
	URL     string
	Current bool
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	p := app.SearchParams{Q: q.Get("q"), Author: q.Get("author"), Advisor: q.Get("advisor"),
		Area: q.Get("area"), Year: q.Get("year"), Type: q.Get("type"), Page: page}
	data := searchData{Params: p, Areas: s.Search.Areas(r.Context()), MaxLen: domain.MaxQueryLen}

	res, err := s.Search.Search(r.Context(), p)
	var verr *app.ValidationError
	switch {
	case errors.As(err, &verr):
		data.Errors = verr.Fields
		s.render(w, r, http.StatusUnprocessableEntity, "search", "Search", data)
		return
	case err != nil:
		data.Failure = "Search temporarily unavailable. No results are shown because none could be retrieved reliably."
		s.render(w, r, http.StatusServiceUnavailable, "search", "Search", data)
		return
	}
	data.Searched, data.Result = true, res
	link := func(n int) string {
		v := url.Values{}
		for k, val := range map[string]string{"q": p.Q, "author": p.Author, "advisor": p.Advisor, "area": p.Area, "year": p.Year, "type": p.Type} {
			if val != "" {
				v.Set(k, val)
			}
		}
		v.Set("page", strconv.Itoa(n))
		return "/?" + v.Encode()
	}
	for n := 1; n <= res.Pages; n++ {
		data.Pages = append(data.Pages, pageLink{n, link(n), n == res.Page})
	}
	if res.Page > 1 {
		data.Prev = link(res.Page - 1)
	}
	if res.Page < res.Pages {
		data.Next = link(res.Page + 1)
	}
	s.render(w, r, http.StatusOK, "search", "Search", data)
}

type detailsData struct {
	Work    domain.Work
	Similar []domain.Work
	Back    string
}

func (s *Server) handleDetails(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		s.fail(w, r, app.ErrNotFound)
		return
	}
	wk, similar, err := s.Search.Details(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	back := r.Referer()
	if u, err := url.Parse(back); err != nil || u.Path != "/" {
		back = "/"
	}
	s.render(w, r, http.StatusOK, "details", wk.Title, detailsData{Work: wk, Similar: similar, Back: back})
}

func (s *Server) handlePublicDocument(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		s.fail(w, r, app.ErrNotFound)
		return
	}
	wk, _, err := s.Search.Details(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if !wk.HasPDF() {
		s.fail(w, r, app.ErrNotFound)
		return
	}
	s.serveDocument(w, r, wk)
}

// ---- F2: staff dashboard, registration and editing ----

type dashboardData struct {
	Rows   []app.DashboardRow
	Status domain.Status
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	status := domain.Status(r.URL.Query().Get("status"))
	rows, err := s.Catalog.Dashboard(r.Context(), s.currentUser(r), status)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, "dashboard", "Staff dashboard", dashboardData{Rows: rows, Status: status})
}

type formData struct {
	ID        int64
	Work      domain.Work // current stored record (zero for new)
	In        app.WorkInput
	Errors    domain.FieldErrors
	Duplicate int64
	Uploaded  bool // a file was sent but not kept because of errors
	MaxPDFMB  int
}

func (d formData) CanSubmit() bool {
	return d.Work.Status == "" || d.Work.Status == domain.StatusDraft || d.Work.Status == domain.StatusRejected
}

func inputFrom(wk domain.Work) app.WorkInput {
	in := app.WorkInput{Title: wk.Title, Type: string(wk.Type), Advisor: wk.Advisor, Abstract: wk.Abstract,
		ShelfLocation: wk.ShelfLocation, ExternalURL: wk.ExternalURL, ContactInfo: wk.ContactInfo,
		ContactPublic: wk.ContactPublic, Version: wk.Version}
	if wk.Year != 0 {
		in.Year = strconv.Itoa(wk.Year)
	}
	in.Authors = strings.Join(wk.Authors, "\n")
	in.Areas = strings.Join(wk.Areas, ", ")
	in.Keywords = strings.Join(wk.Keywords, ", ")
	return in
}

func (s *Server) requireStaff(w http.ResponseWriter, r *http.Request, roles ...domain.Role) (*domain.User, bool) {
	u := s.currentUser(r)
	if u == nil {
		s.fail(w, r, app.ErrUnauthenticated)
		return nil, false
	}
	for _, role := range roles {
		if u.Role == role {
			return u, true
		}
	}
	s.fail(w, r, app.ErrForbidden)
	return nil, false
}

func (s *Server) handleNewForm(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireStaff(w, r, domain.RoleLibraryWorker); !ok {
		return
	}
	s.render(w, r, http.StatusOK, "form", "New academic work", formData{MaxPDFMB: domain.MaxPDFBytes >> 20})
}

func (s *Server) handleEditForm(w http.ResponseWriter, r *http.Request) {
	u, ok := s.requireStaff(w, r, domain.RoleLibraryWorker)
	if !ok {
		return
	}
	id, _ := pathID(r)
	v, err := s.Catalog.View(r.Context(), u, id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if v.Work.Status == domain.StatusUnderReview {
		s.fail(w, r, &app.TransitionError{From: v.Work.Status, To: v.Work.Status})
		return
	}
	s.render(w, r, http.StatusOK, "form", "Edit work #"+strconv.FormatInt(id, 10),
		formData{ID: id, Work: v.Work, In: inputFrom(v.Work), MaxPDFMB: domain.MaxPDFBytes >> 20})
}

func (s *Server) handleSave(w http.ResponseWriter, r *http.Request) {
	// Authorization is enforced again inside the service (NFR-03); this early
	// check just avoids reading uploads from users who may not write.
	u, ok := s.requireStaff(w, r, domain.RoleLibraryWorker)
	if !ok {
		return
	}
	var id int64
	if r.PathValue("id") != "" {
		if id, ok = pathID(r); !ok {
			s.fail(w, r, app.ErrNotFound)
			return
		}
	}
	r.Body = http.MaxBytesReader(w, r.Body, domain.MaxPDFBytes+(2<<20))
	if err := r.ParseMultipartForm(8 << 20); err != nil && !errors.Is(err, http.ErrNotMultipart) {
		s.errorPage(w, r, http.StatusRequestEntityTooLarge, "Upload too large",
			fmt.Sprintf("Uploads are limited to %d MB.", domain.MaxPDFBytes>>20))
		return
	}
	version, _ := strconv.Atoi(r.FormValue("version"))
	in := app.WorkInput{
		Title: r.FormValue("title"), Year: r.FormValue("year"), Type: r.FormValue("type"),
		Authors: r.FormValue("authors"), Advisor: r.FormValue("advisor"), Areas: r.FormValue("areas"),
		Keywords: r.FormValue("keywords"), Abstract: r.FormValue("abstract"),
		ShelfLocation: r.FormValue("shelf_location"), ExternalURL: r.FormValue("external_url"),
		ContactInfo: r.FormValue("contact_info"), ContactPublic: r.FormValue("contact_public") == "on",
		RemovePDF: r.FormValue("remove_pdf") == "on", Version: version,
	}
	up, err := readUpload(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	submit := r.FormValue("action") == "submit"

	savedID, err := s.Catalog.Save(r.Context(), u, id, in, up, submit)
	if err != nil {
		var verr *app.ValidationError
		var dup *app.DuplicateError
		data := formData{ID: id, In: in, Uploaded: up != nil, MaxPDFMB: domain.MaxPDFBytes >> 20}
		if id != 0 {
			if v, verr := s.Catalog.View(r.Context(), u, id); verr == nil {
				data.Work = v.Work
			}
		}
		switch {
		case errors.As(err, &verr):
			data.Errors = verr.Fields
			s.render(w, r, http.StatusUnprocessableEntity, "form", "Fix the highlighted fields", data)
		case errors.As(err, &dup):
			data.Duplicate = dup.ExistingID
			s.render(w, r, http.StatusConflict, "form", "Probable duplicate", data)
		default:
			s.fail(w, r, err)
		}
		return
	}
	flash := "draft"
	switch {
	case submit:
		flash = "submitted"
	case id != 0:
		if v, err := s.Catalog.View(r.Context(), u, savedID); err == nil && v.Work.Status != domain.StatusDraft {
			flash = "saved"
		}
	}
	http.Redirect(w, r, withFlash(fmt.Sprintf("/staff/works/%d", savedID), flash), http.StatusSeeOther)
}

type workData struct {
	app.StaffView
	CanEdit     bool
	CanModerate bool
	Errors      domain.FieldErrors
	Reason      string
}

func (s *Server) handleStaffWork(w http.ResponseWriter, r *http.Request) {
	id, _ := pathID(r)
	u := s.currentUser(r)
	v, err := s.Catalog.View(r.Context(), u, id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, "work", v.Work.Title, s.workData(u, v))
}

func (s *Server) workData(u *domain.User, v app.StaffView) workData {
	return workData{StaffView: v,
		CanEdit:     u.Role == domain.RoleLibraryWorker && v.Work.Status != domain.StatusUnderReview,
		CanModerate: u.Role == domain.RoleCoordinator && v.Work.Status == domain.StatusUnderReview,
	}
}

func (s *Server) handleStaffDocument(w http.ResponseWriter, r *http.Request) {
	id, _ := pathID(r)
	v, err := s.Catalog.View(r.Context(), s.currentUser(r), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if !v.Work.HasPDF() {
		s.fail(w, r, app.ErrNotFound)
		return
	}
	s.serveDocument(w, r, v.Work)
}

// ---- F2 support: moderation ----

func (s *Server) handleQueue(w http.ResponseWriter, r *http.Request) {
	works, err := s.Moderation.Queue(r.Context(), s.currentUser(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, "queue", "Moderation queue", works)
}

func (s *Server) handleDecide(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		s.fail(w, r, app.ErrNotFound)
		return
	}
	u := s.currentUser(r)
	version, _ := strconv.Atoi(r.FormValue("version"))
	approve := r.FormValue("decision") == "approve"
	reason := r.FormValue("reason")

	err := s.Moderation.Decide(r.Context(), u, id, version, approve, reason)
	var verr *app.ValidationError
	if errors.As(err, &verr) {
		v, vErr := s.Catalog.View(r.Context(), u, id)
		if vErr != nil {
			s.fail(w, r, vErr)
			return
		}
		data := s.workData(u, v)
		data.Errors, data.Reason = verr.Fields, reason
		s.render(w, r, http.StatusUnprocessableEntity, "work", v.Work.Title, data)
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	flash := "approved"
	if !approve {
		flash = "rejected"
	}
	http.Redirect(w, r, withFlash("/review", flash), http.StatusSeeOther)
}
