package domain

import (
	"bytes"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Boundary values chosen for the prototype where Stage 1 left them open (Section 9.1).
const (
	MaxQueryLen = 200
	MinYear     = 1950
	MaxPDFBytes = 20 << 20
)

// FieldErrors maps a form field name to a human-readable message.
type FieldErrors map[string]string

func (f FieldErrors) Any() bool { return len(f) > 0 }

// CheckQueryLength enforces the free-text maximum (AC-01d).
func CheckQueryLength(s string) string {
	if utf8.RuneCountInString(s) > MaxQueryLen {
		return fmt.Sprintf("Maximum length is %d characters.", MaxQueryLen)
	}
	return ""
}

// ParseYear parses a cataloguing year; blank yields 0 with no error.
func ParseYear(s string, currentYear int) (int, string) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, ""
	}
	y, err := strconv.Atoi(s)
	if err != nil || len(s) != 4 {
		return 0, "Year must be a four-digit number."
	}
	if y < MinYear || y > currentYear {
		return 0, fmt.Sprintf("Year must be between %d and %d.", MinYear, currentYear)
	}
	return y, ""
}

// CheckURL accepts well-formed absolute http(s) URLs only.
func CheckURL(s string) string {
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || strings.ContainsAny(s, " \t\n") {
		return "Enter a valid http(s) URL to an authoritative repository."
	}
	return ""
}

// CheckPDF validates an uploaded document by extension, sniffed MIME type and size (NFR-03).
func CheckPDF(name string, data []byte) string {
	if len(data) == 0 {
		return "The uploaded file is empty."
	}
	if len(data) > MaxPDFBytes {
		return fmt.Sprintf("The PDF exceeds the %d MB limit.", MaxPDFBytes>>20)
	}
	if strings.ToLower(filepath.Ext(name)) != ".pdf" ||
		!bytes.HasPrefix(data, []byte("%PDF-")) ||
		http.DetectContentType(data) != "application/pdf" {
		return "Unsupported file type: only PDF documents are accepted."
	}
	return ""
}

// ValidateForSubmission applies FR-W-06 / BR-04. Year format is checked when
// parsing the form, so here only its presence matters.
func ValidateForSubmission(w Work) FieldErrors {
	errs := FieldErrors{}
	if strings.TrimSpace(w.Title) == "" {
		errs["title"] = "Title is required."
	}
	if w.Year == 0 {
		errs["year"] = "Year is required."
	}
	if !w.Type.Valid() {
		errs["type"] = "Choose TCC, dissertation or thesis."
	}
	if len(w.Authors) == 0 || strings.TrimSpace(w.Authors[0]) == "" {
		errs["authors"] = "At least one author is required."
	}
	if strings.TrimSpace(w.Advisor) == "" {
		errs["advisor"] = "An advisor is required."
	}
	if len(w.Areas) == 0 {
		errs["areas"] = "At least one research area is required."
	}
	if len(w.Keywords) == 0 {
		errs["keywords"] = "At least one keyword is required."
	}
	if strings.TrimSpace(w.Abstract) == "" {
		errs["abstract"] = "Abstract is required."
	}
	if !w.HasPDF() && w.ExternalURL == "" {
		errs["source"] = "Attach a PDF or provide an external full-text URL."
	}
	return errs
}
