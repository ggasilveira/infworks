// Package app contains the application services (search, cataloguing,
// moderation) and the ports they need from infrastructure.
package app

import (
	"context"
	"errors"
	"fmt"

	"infworks/internal/domain"
)

var (
	ErrUnauthenticated = errors.New("authentication required")
	ErrForbidden       = errors.New("not allowed for this role")
	ErrNotFound        = errors.New("record not found")
	ErrConflict        = errors.New("the record was changed or already decided by someone else")
	ErrUnavailable     = errors.New("service temporarily unavailable")
)

// ValidationError carries field-level messages (HTTP 422).
type ValidationError struct{ Fields domain.FieldErrors }

func (e *ValidationError) Error() string { return "validation failed" }

// DuplicateError points to the existing record that blocks a submission (HTTP 409).
type DuplicateError struct{ ExistingID int64 }

func (e *DuplicateError) Error() string {
	return fmt.Sprintf("probable duplicate of record #%d", e.ExistingID)
}

// TransitionError reports a status change forbidden by Table 6.
type TransitionError struct{ From, To domain.Status }

func (e *TransitionError) Error() string {
	return fmt.Sprintf("transition %s -> %s is not permitted", e.From, e.To)
}

type SearchQuery struct {
	Q, Author, Advisor, Area string
	Year                     int
	Type                     domain.WorkType
	Offset, Limit            int
}

// Repository is the persistence port. Implementations must return ErrNotFound
// for missing records and ErrConflict when a guarded update matches no row.
type Repository interface {
	GetWork(ctx context.Context, id int64) (domain.Work, error)
	CreateWork(ctx context.Context, w domain.Work, audit domain.AuditEntry) (int64, error)
	// UpdateWork stores w only if the row still has w.ID, expectStatus and w.Version.
	UpdateWork(ctx context.Context, w domain.Work, expectStatus domain.Status, audit domain.AuditEntry) error
	// NonDraftWorksByYear returns non-draft works of a year, for the duplicate rule.
	NonDraftWorksByYear(ctx context.Context, year int, excludeID int64) ([]domain.Work, error)
	SearchPublished(ctx context.Context, q SearchQuery) ([]domain.Work, int, error)
	// PublishedSharingKeywords returns published works with at least one keyword in common.
	PublishedSharingKeywords(ctx context.Context, keywords []string, excludeID int64) ([]domain.Work, error)
	PublishedAreas(ctx context.Context) ([]string, error)
	ListWorks(ctx context.Context, status domain.Status) ([]domain.Work, error)
	// RecordDecision sets the new status only if the work is UNDER_REVIEW at version.
	RecordDecision(ctx context.Context, version int, d domain.ReviewDecision, to domain.Status) error
	Decisions(ctx context.Context, workID int64) ([]domain.ReviewDecision, error)
	LatestDecisions(ctx context.Context) (map[int64]domain.ReviewDecision, error)
	Audit(ctx context.Context, workID int64) ([]domain.AuditEntry, error)
}

// DocumentStore keeps uploaded PDFs.
type DocumentStore interface {
	Save(data []byte) (string, error)
	Remove(name string) error
}

// unavailable wraps infrastructure failures so callers can show NFR-06 messages.
func unavailable(err error) error {
	if err == nil || errors.Is(err, ErrNotFound) || errors.Is(err, ErrConflict) {
		return err
	}
	return fmt.Errorf("%w: %v", ErrUnavailable, err)
}

func requireRole(u *domain.User, roles ...domain.Role) error {
	if u == nil {
		return ErrUnauthenticated
	}
	for _, r := range roles {
		if u.Role == r {
			return nil
		}
	}
	return ErrForbidden
}
