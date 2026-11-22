package domain

type transition struct {
	from, to Status
	role     Role
}

// allowed mirrors Table 6. The empty "from" status means a new record.
// REJECTED->REJECTED lets staff save corrections before resubmitting.
var allowed = map[transition]bool{
	{"", StatusDraft, RoleLibraryWorker}:                   true,
	{StatusDraft, StatusDraft, RoleLibraryWorker}:          true,
	{StatusRejected, StatusRejected, RoleLibraryWorker}:    true,
	{"", StatusUnderReview, RoleLibraryWorker}:             true,
	{StatusDraft, StatusUnderReview, RoleLibraryWorker}:    true,
	{StatusRejected, StatusUnderReview, RoleLibraryWorker}: true,
	{StatusUnderReview, StatusPublished, RoleCoordinator}:  true,
	{StatusUnderReview, StatusRejected, RoleCoordinator}:   true,
	{StatusPublished, StatusPublished, RoleLibraryWorker}:  true,
}

// CanTransition reports whether role may move a work from one status to another.
func CanTransition(from, to Status, role Role) bool {
	return allowed[transition{from, to, role}]
}
