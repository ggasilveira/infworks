package app

import (
	"context"
	"sort"
	"strings"
	"time"

	"infworks/internal/domain"
)

const (
	PageSize       = 10
	MaxSimilarWork = 5
)

// SearchParams is raw visitor input (UC-01).
type SearchParams struct {
	Q, Author, Advisor, Area, Year, Type string
	Page                                 int
}

type SearchResult struct {
	Works       []domain.Work
	Total, Page int
	Pages       int
}

type Search struct {
	Repo Repository
	Now  func() time.Time
}

// Search validates input and returns one page of published matches (FR-S-01..05, 10, 11).
func (s *Search) Search(ctx context.Context, p SearchParams) (SearchResult, error) {
	errs := domain.FieldErrors{}
	for field, v := range map[string]string{"q": p.Q, "author": p.Author, "advisor": p.Advisor, "area": p.Area} {
		if msg := domain.CheckQueryLength(v); msg != "" {
			errs[field] = msg
		}
	}
	year, msg := domain.ParseYear(p.Year, s.Now().Year())
	if msg != "" {
		errs["year"] = msg
	}
	t := domain.WorkType(p.Type)
	if p.Type != "" && !t.Valid() {
		errs["type"] = "Unknown document type."
	}
	if errs.Any() {
		return SearchResult{}, &ValidationError{errs}
	}
	page := max(p.Page, 1)
	works, total, err := s.Repo.SearchPublished(ctx, SearchQuery{
		Q: strings.TrimSpace(p.Q), Author: strings.TrimSpace(p.Author),
		Advisor: strings.TrimSpace(p.Advisor), Area: strings.TrimSpace(p.Area),
		Year: year, Type: t, Offset: (page - 1) * PageSize, Limit: PageSize,
	})
	if err != nil {
		return SearchResult{}, unavailable(err)
	}
	return SearchResult{Works: works, Total: total, Page: page, Pages: (total + PageSize - 1) / PageSize}, nil
}

// Details returns a published work and up to five similar published works (FR-S-06..09).
func (s *Search) Details(ctx context.Context, id int64) (domain.Work, []domain.Work, error) {
	w, err := s.Repo.GetWork(ctx, id)
	if err != nil {
		return domain.Work{}, nil, unavailable(err)
	}
	if w.Status != domain.StatusPublished {
		return domain.Work{}, nil, ErrNotFound
	}
	candidates, err := s.Repo.PublishedSharingKeywords(ctx, w.Keywords, w.ID)
	if err != nil {
		// Similar works are optional: omit the section rather than fail the page.
		return w, nil, nil
	}
	return w, rankSimilar(w, candidates), nil
}

func (s *Search) Areas(ctx context.Context) []string {
	areas, _ := s.Repo.PublishedAreas(ctx)
	return areas
}

// rankSimilar orders by keyword overlap, then most recent, then ID for stability.
func rankSimilar(w domain.Work, candidates []domain.Work) []domain.Work {
	mine := map[string]bool{}
	for _, k := range w.Keywords {
		mine[domain.Normalize(k)] = true
	}
	overlap := map[int64]int{}
	var out []domain.Work
	for _, c := range candidates {
		if c.ID == w.ID || c.Status != domain.StatusPublished {
			continue
		}
		for _, k := range c.Keywords {
			if mine[domain.Normalize(k)] {
				overlap[c.ID]++
			}
		}
		if overlap[c.ID] > 0 {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if overlap[a.ID] != overlap[b.ID] {
			return overlap[a.ID] > overlap[b.ID]
		}
		if a.Year != b.Year {
			return a.Year > b.Year
		}
		return a.ID < b.ID
	})
	if len(out) > MaxSimilarWork {
		out = out[:MaxSimilarWork]
	}
	return out
}
