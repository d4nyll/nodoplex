package graph

import "fmt"

// The refusal codes of Appendix A of the base specification, and of the
// extended specification.
const (
	CodeInvalidRequest       = "invalid_request"
	CodeNotFound             = "not_found"
	CodeMethodNotAllowed     = "method_not_allowed"
	CodeSlugTaken            = "slug_taken"
	CodeInvalidReference     = "invalid_reference"
	CodeAlreadyConnected     = "already_connected"
	CodeCycle                = "cycle"
	CodeParentRequired       = "parent_required"
	CodeHasIncomingEdges     = "has_incoming_edges"
	CodeVersionConflict      = "version_conflict"
	CodeStatusIsDerived      = "status_is_derived"
	CodeDoneLeafBlocked      = "done_leaf_blocked"
	CodePriorityOutOfRange   = "priority_out_of_range"
	CodeChangefeedDisabled   = "changefeed_disabled"
	CodeChangeOutOfRange     = "change_out_of_range"
	CodeChangeEntriesDeleted = "change_entries_deleted"
	CodeUnavailable          = "unavailable"
	CodeInternalError        = "internal_error"
)

// Error is a refusal with a code.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func Refuse(code, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

func invalid(format string, args ...any) *Error {
	return Refuse(CodeInvalidRequest, format, args...)
}
