package contract

import (
	"errors"
	"fmt"
	"net/http"
	"sort"
)

// Code is a stable, machine-readable error identifier. Clients switch on it;
// messages are for humans and may change.
type Code string

// The registry. Add a code here (and to errorSpecs) before returning it from
// any handler or subcommand; TestRegistryIsComplete fails otherwise.
const (
	CodeInvalidRequest  Code = "invalid_request"
	CodeUnauthenticated Code = "unauthenticated"
	CodeForbidden       Code = "forbidden"
	CodeNotFound        Code = "not_found"
	CodeConflict        Code = "conflict"
	CodeGone            Code = "gone"
	CodeRateLimited     Code = "rate_limited"
	CodeConfigInvalid   Code = "config_invalid"
	CodeNotAvailable    Code = "not_available"
	CodeUnavailable     Code = "unavailable"
	CodeInternal        Code = "internal"

	CodeNotInvited          Code = "not_invited"
	CodeWorkspaceLimit      Code = "workspace_limit"
	CodeDefaultWorkspace    Code = "default_workspace"
	CodeDuplicateInvitation Code = "duplicate_invitation"
	CodeValidationFailed    Code = "validation_failed"
	CodeLastAdmin           Code = "last_admin"
	CodeAlreadyMember       Code = "already_member"
	CodeGroupNameTaken      Code = "group_name_taken"
)

// ErrorSpec is the registry entry for one code.
type ErrorSpec struct {
	Code   Code   `json:"code"`
	Status int    `json:"status"`
	Doc    string `json:"doc"`
}

var errorSpecs = []ErrorSpec{
	{CodeInvalidRequest, http.StatusBadRequest, "The request is malformed or fails validation; details lists the fields."},
	{CodeUnauthenticated, http.StatusUnauthorized, "No valid credential was presented."},
	{CodeForbidden, http.StatusForbidden, "The caller is authenticated but lacks the permission."},
	{CodeNotFound, http.StatusNotFound, "The resource does not exist or is not visible to the caller."},
	{CodeConflict, http.StatusConflict, "The request conflicts with current state."},
	{CodeGone, http.StatusGone, "The resource existed and was retired (for example a reset workspace URL)."},
	{CodeRateLimited, http.StatusTooManyRequests, "A configured operational limit was hit."},
	{CodeConfigInvalid, http.StatusInternalServerError, "The process configuration is invalid; details lists every problem."},
	{CodeNotAvailable, http.StatusNotImplemented, "The capability is not wired in this build."},
	{CodeUnavailable, http.StatusServiceUnavailable, "A dependency (for example the database) is not reachable right now; retry."},
	{CodeInternal, http.StatusInternalServerError, "An unexpected failure; the message carries no internals."},
	{CodeNotInvited, http.StatusForbidden, "The identity is valid but holds no active membership and no pending invitation."},
	{CodeWorkspaceLimit, http.StatusConflict, "The organization already has MAX_WORKSPACES workspaces."},
	{CodeDefaultWorkspace, http.StatusConflict, "The default workspace cannot be deleted."},
	{CodeDuplicateInvitation, http.StatusConflict, "A pending invitation already exists for that email."},
	{CodeValidationFailed, http.StatusUnprocessableEntity, "A request field failed validation; details lists the fields."},
	{CodeLastAdmin, http.StatusConflict, "The change would leave the organization without an active admin."},
	{CodeAlreadyMember, http.StatusConflict, "The email already belongs to an active member."},
	{CodeGroupNameTaken, http.StatusConflict, "A group with that name already exists."},
}

// Errors returns the registry sorted by code. The slice is a copy.
func Errors() []ErrorSpec {
	out := append([]ErrorSpec(nil), errorSpecs...)
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out
}

// Spec looks up a code; ok is false for an unregistered one.
func Spec(c Code) (ErrorSpec, bool) {
	for _, s := range errorSpecs {
		if s.Code == c {
			return s, true
		}
	}
	return ErrorSpec{}, false
}

// Error is the one error type that crosses a package boundary toward a client.
// Its JSON form is the API error body: {code, message, details}.
type Error struct {
	Code    Code     `json:"code"`
	Message string   `json:"message"`
	Details []string `json:"details,omitempty"`
	// Reason is the machine-readable why of a 403 ("no matching grant", "admin required"); it
	// travels as details.reason. Empty for every other error.
	Reason string `json:"-"`
}

// Error implements error.
func (e *Error) Error() string {
	if len(e.Details) == 0 {
		return fmt.Sprintf("%s: %s", e.Code, e.Message)
	}
	return fmt.Sprintf("%s: %s (%d detail(s))", e.Code, e.Message, len(e.Details))
}

// Status is the HTTP status the registry assigns to the code; unregistered
// codes map to 500 so a typo never leaks as a success.
func (e *Error) Status() int {
	if s, ok := Spec(e.Code); ok {
		return s.Status
	}
	return http.StatusInternalServerError
}

// NewError builds an *Error.
func NewError(c Code, message string, details ...string) *Error {
	return &Error{Code: c, Message: message, Details: details}
}

// AsError unwraps err to an *Error; any other error becomes CodeInternal with
// a generic message, so internals never reach a client.
func AsError(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return NewError(CodeInternal, "internal error")
}

// Body is the API error body for e. Detail strings (a config check lists every
// problem) travel as details.problems; no detail means an empty object.
func (e *Error) Body() ErrorBody {
	d := map[string]any{}
	if len(e.Details) > 0 {
		d["problems"] = append([]string(nil), e.Details...)
	}
	if e.Reason != "" {
		d["reason"] = e.Reason
	}
	return ErrorBody{Code: e.Code, Message: e.Message, Details: d}
}

// Forbidden builds the 403 forbidden error with a reason (details.reason).
func Forbidden(message, reason string) *Error {
	return &Error{Code: CodeForbidden, Message: message, Reason: reason}
}
