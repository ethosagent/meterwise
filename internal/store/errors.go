package store

import "errors"

// Class names why a store operation failed, without any connection detail.
type Class string

// The failure classes. Driver and goose error text can carry the DSN, a host or
// a password (pgx echoes parts of the URL), so the store never returns it:
// callers get a Class and a fixed message.
const (
	ClassBadURL      Class = "bad_url"
	ClassUnreachable Class = "unreachable"
	ClassMigration   Class = "migration_failed"
	ClassPool        Class = "pool"
)

// Error is the only error type Open, Migrate, Status and Pending return. It does
// not wrap the cause, so nothing from the driver survives errors.As or %v.
type Error struct {
	Class   Class
	Message string

	transient bool // a server error that means the database is briefly away (see IsTransient)
}

// Error implements error with the fixed message.
func (e *Error) Error() string { return e.Message }

func fail(c Class, msg string) *Error { return &Error{Class: c, Message: msg} }

// ClassOf returns the class of err, or "" when it is not a store *Error.
func ClassOf(err error) Class {
	var e *Error
	if errors.As(err, &e) {
		return e.Class
	}
	return ""
}
