package domain

import (
	"strings"
	"testing"
)

func completeWork() Work {
	return Work{Title: "Graph Search", Year: 2024, Type: TypeTCC, Authors: []string{"Ana"}, Advisor: "Bia",
		Areas: []string{"AI"}, Keywords: []string{"graphs"}, Abstract: "abs", ExternalURL: "https://example.org/x"}
}

// TS-04, TS-05, TS-06: free-text length boundary at 200 characters (AC-01d).
func TestQueryLengthBoundary(t *testing.T) {
	for n, ok := range map[int]bool{0: true, 199: true, 200: true, 201: false} {
		if got := CheckQueryLength(strings.Repeat("a", n)) == ""; got != ok {
			t.Errorf("length %d: accepted=%v, want %v", n, got, ok)
		}
	}
	// Characters, not bytes: 200 accented letters are still valid.
	if CheckQueryLength(strings.Repeat("ç", 200)) != "" {
		t.Error("200 multibyte characters should be accepted")
	}
}

// TW-02 baseline plus TW-03..TW-09 and TW-13: each missing mandatory field is reported.
func TestValidateForSubmission(t *testing.T) {
	if errs := ValidateForSubmission(completeWork()); errs.Any() {
		t.Fatalf("complete work rejected: %v", errs)
	}
	cases := map[string]func(*Work){
		"title":    func(w *Work) { w.Title = "   " },                   // TW-03
		"year":     func(w *Work) { w.Year = 0 },                        // TW-04
		"type":     func(w *Work) { w.Type = "ARTICLE" },                // out of scope type
		"authors":  func(w *Work) { w.Authors = nil },                   // TW-06
		"advisor":  func(w *Work) { w.Advisor = "" },                    // TW-07
		"areas":    func(w *Work) { w.Areas = nil },                     // TW-08
		"keywords": func(w *Work) { w.Keywords = nil },                  // TW-09
		"abstract": func(w *Work) { w.Abstract = "" },                   // FR-W-06
		"source":   func(w *Work) { w.ExternalURL, w.PDFFile = "", "" }, // TW-13
	}
	for field, mutate := range cases {
		w := completeWork()
		mutate(&w)
		errs := ValidateForSubmission(w)
		if _, ok := errs[field]; !ok || len(errs) != 1 {
			t.Errorf("%s: got errors %v, want exactly one on %q", field, errs, field)
		}
	}
}

// TW-05: invalid and boundary years.
func TestParseYear(t *testing.T) {
	cases := map[string]bool{"2026": true, "1950": true, "": true, "1949": false, "2027": false, "abcd": false, "0000": false, "26": false}
	for in, ok := range cases {
		_, msg := ParseYear(in, 2026)
		if (msg == "") != ok {
			t.Errorf("ParseYear(%q): msg=%q, want ok=%v", in, msg, ok)
		}
	}
}

// TW-10, TW-14: genuine PDF accepted; DOCX, renamed files and empty files rejected.
func TestCheckPDF(t *testing.T) {
	pdf := []byte("%PDF-1.4\n1 0 obj << >> endobj\n%%EOF")
	if msg := CheckPDF("thesis.pdf", pdf); msg != "" {
		t.Errorf("valid PDF rejected: %s", msg)
	}
	docx := []byte("PK\x03\x04 word/document.xml")
	for name, data := range map[string][]byte{"thesis.docx": docx, "fake.pdf": docx, "pdf-named.docx": pdf, "empty.pdf": nil} {
		if CheckPDF(name, data) == "" {
			t.Errorf("%s should be rejected", name)
		}
	}
	if CheckPDF("big.pdf", append(pdf, make([]byte, MaxPDFBytes)...)) == "" {
		t.Error("oversized PDF should be rejected")
	}
}

// TW-11, TW-15: external URL validation.
func TestCheckURL(t *testing.T) {
	for u, ok := range map[string]bool{
		"https://lume.example.org/handle/1": true, "http://example.org/x.pdf": true,
		"ftp://example.org/x": false, "example.org/x": false, "https://": false, "not a url": false,
		"javascript:alert(1)": false,
	} {
		if (CheckURL(u) == "") != ok {
			t.Errorf("CheckURL(%q) ok=%v", u, !ok)
		}
	}
}

// TW-17, TW-18: duplicate key normalization.
func TestDuplicateKey(t *testing.T) {
	a := Work{Title: "Graph Search", Year: 2024, Authors: []string{"José Araújo"}}
	same := Work{Title: "  GRAPH   search ", Year: 2024, Authors: []string{"jose araujo"}}
	if DuplicateKey(a) != DuplicateKey(same) {
		t.Error("case/space/diacritic variants should share a key")
	}
	otherYear, otherAuthor := a, a
	otherYear.Year = 2023
	otherAuthor.Authors = []string{"Maria"}
	if DuplicateKey(a) == DuplicateKey(otherYear) || DuplicateKey(a) == DuplicateKey(otherAuthor) {
		t.Error("different year or first author must not match")
	}
}

// TW-24 and Table 6: permitted and forbidden transitions.
func TestTransitions(t *testing.T) {
	ok := []struct {
		from, to Status
		role     Role
	}{
		{StatusDraft, StatusUnderReview, RoleLibraryWorker},
		{StatusRejected, StatusUnderReview, RoleLibraryWorker},
		{StatusUnderReview, StatusPublished, RoleCoordinator},
		{StatusUnderReview, StatusRejected, RoleCoordinator},
		{StatusPublished, StatusPublished, RoleLibraryWorker},
	}
	for _, c := range ok {
		if !CanTransition(c.from, c.to, c.role) {
			t.Errorf("%s->%s by %s should be allowed", c.from, c.to, c.role)
		}
	}
	bad := []struct {
		from, to Status
		role     Role
	}{
		{StatusDraft, StatusPublished, RoleLibraryWorker},
		{StatusDraft, StatusPublished, RoleCoordinator},
		{StatusRejected, StatusPublished, RoleCoordinator},
		{StatusUnderReview, StatusPublished, RoleLibraryWorker},
		{StatusDraft, StatusUnderReview, RoleCoordinator},
		{StatusDraft, StatusUnderReview, RoleResearcher},
		{StatusPublished, StatusUnderReview, RoleLibraryWorker},
		{StatusUnderReview, StatusUnderReview, RoleLibraryWorker},
	}
	for _, c := range bad {
		if CanTransition(c.from, c.to, c.role) {
			t.Errorf("%s->%s by %s should be forbidden", c.from, c.to, c.role)
		}
	}
}
