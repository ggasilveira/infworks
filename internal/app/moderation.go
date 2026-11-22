package app

import (
	"context"
	"strings"
	"time"

	"infworks/internal/domain"
)

type Moderation struct {
	Repo Repository
	Now  func() time.Time
}

func (m *Moderation) Queue(ctx context.Context, u *domain.User) ([]domain.Work, error) {
	if err := requireRole(u, domain.RoleCoordinator); err != nil {
		return nil, err
	}
	works, err := m.Repo.ListWorks(ctx, domain.StatusUnderReview)
	return works, unavailable(err)
}

// Decide approves or rejects a work under review (UC-03, FR-W-09, BR-06).
// version is the record version the coordinator inspected; a stale version
// or an already-resolved record yields ErrConflict.
func (m *Moderation) Decide(ctx context.Context, u *domain.User, id int64, version int, approve bool, reason string) error {
	if err := requireRole(u, domain.RoleCoordinator); err != nil {
		return err
	}
	w, err := m.Repo.GetWork(ctx, id)
	if err != nil {
		return unavailable(err)
	}
	if w.Status != domain.StatusUnderReview || w.Version != version {
		return ErrConflict
	}
	d := domain.ReviewDecision{WorkID: id, Moderator: u.Username, DecidedAt: m.Now(), Decision: domain.DecisionApproved}
	to := domain.StatusPublished
	if !approve {
		reason = strings.TrimSpace(reason)
		if reason == "" {
			return &ValidationError{domain.FieldErrors{"reason": "A rejection reason is required."}}
		}
		d.Decision, d.Reason, to = domain.DecisionRejected, reason, domain.StatusRejected
	}
	if !domain.CanTransition(w.Status, to, u.Role) {
		return &TransitionError{From: w.Status, To: to}
	}
	return unavailable(m.Repo.RecordDecision(ctx, version, d, to))
}
