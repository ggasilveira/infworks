// Package domain holds the core model of the INF knowledge base: academic
// works, their lifecycle, validation rules and duplicate normalization.
package domain

import "time"

// WorkType is the kind of academic work supported by the MVP.
type WorkType string

const (
	TypeTCC          WorkType = "TCC"
	TypeDissertation WorkType = "DISSERTATION"
	TypeThesis       WorkType = "THESIS"
)

// WorkTypes lists the supported types in display order.
var WorkTypes = []WorkType{TypeTCC, TypeDissertation, TypeThesis}

func (t WorkType) Valid() bool {
	for _, v := range WorkTypes {
		if t == v {
			return true
		}
	}
	return false
}

func (t WorkType) Label() string {
	switch t {
	case TypeTCC:
		return "Undergraduate final project (TCC)"
	case TypeDissertation:
		return "Master's dissertation"
	case TypeThesis:
		return "Doctoral thesis"
	}
	return string(t)
}

// Status is the lifecycle state of an academic work (Section 8.5).
type Status string

const (
	StatusDraft       Status = "DRAFT"
	StatusUnderReview Status = "UNDER_REVIEW"
	StatusPublished   Status = "PUBLISHED"
	StatusRejected    Status = "REJECTED"
)

var Statuses = []Status{StatusDraft, StatusUnderReview, StatusPublished, StatusRejected}

// Role is an application role; authentication is external, authorization is ours.
type Role string

const (
	RoleLibraryWorker Role = "LIBRARY_WORKER"
	RoleCoordinator   Role = "COORDINATOR"
	RoleResearcher    Role = "RESEARCHER"
)

type User struct {
	Username    string
	DisplayName string
	Role        Role
}

// Work is an academic work record. Drafts may leave any field empty.
type Work struct {
	ID            int64
	Title         string
	Year          int // 0 when not set
	Type          WorkType
	Status        Status
	Authors       []string
	Advisor       string
	Areas         []string
	Keywords      []string
	Abstract      string
	ShelfLocation string
	ExternalURL   string
	PDFFile       string // stored file name in document storage
	PDFName       string // original upload name
	ContactInfo   string
	ContactPublic bool
	Version       int
	CreatedBy     string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

func (w Work) FirstAuthor() string {
	if len(w.Authors) == 0 {
		return ""
	}
	return w.Authors[0]
}

func (w Work) HasPDF() bool { return w.PDFFile != "" }

// Decision is a coordinator's moderation outcome.
type Decision string

const (
	DecisionApproved Decision = "APPROVED"
	DecisionRejected Decision = "REJECTED"
)

type ReviewDecision struct {
	ID        int64
	WorkID    int64
	Decision  Decision
	Reason    string
	Moderator string
	DecidedAt time.Time
}

type AuditEntry struct {
	ID     int64
	WorkID int64
	Actor  string
	Action string
	Detail string
	At     time.Time
}
