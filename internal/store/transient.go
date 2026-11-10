package store

import (
	"errors"
	"net"
	"strings"
	"sync/atomic"

	"github.com/jackc/pgx/v5/pgconn"
)

// IsTransient reports whether err looks like the database being briefly away (a restart, a failover, a
// connection cut or refused), so the same operation is worth retrying later. It is true for a store
// error of class unreachable, for dial and other network errors (a connect timeout included), and for
// Postgres server errors that mean "not accepting connections or going away": SQLSTATE class 08,
// 57P01 admin_shutdown, 57P02 crash_shutdown, 57P03 cannot_connect_now, 53300 too_many_connections,
// and 55000 only when it says the database is not accepting connections. Any other server error (a
// constraint, a bad value) is not transient. It does not change how errors are classified for the API.
//
// The list is explicit on purpose: inverting it to "everything but 22/23/42" would retry a permanent
// error for the whole retry window on every batch and stall the queue. A client-side error that says
// the same thing in the driver's own words counts too: pgconn.ConnectError (every connect attempt
// failed) and anything pgconn.SafeToRetry vouches for (nothing reached the server). context.DeadlineExceeded
// is a net.Error, so a connect or query deadline is transient; it is bounded by the ledger's 30 s retry window.
func IsTransient(err error) bool {
	if err == nil {
		return false
	}
	var own *Error
	if errors.As(err, &own) {
		return own.Class == ClassUnreachable || own.transient
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return transientCode(pg.Code, pg.Message)
	}
	var ce *pgconn.ConnectError
	if errors.As(err, &ce) || pgconn.SafeToRetry(err) {
		return true
	}
	var ne net.Error
	return errors.As(err, &ne)
}

// unexpected55000 counts server errors with SQLSTATE 55000 whose message is not the one we treat as
// "not accepting connections" (a wording change in a new Postgres would otherwise turn an outage into
// a permanent error silently). /statusz shows it through UnexpectedStateErrors.
var unexpected55000 atomic.Int64

// UnexpectedStateErrors is how many 55000 errors with an unrecognised message the store has seen.
func UnexpectedStateErrors() int64 { return unexpected55000.Load() }

func transientCode(code, msg string) bool {
	switch {
	case strings.HasPrefix(code, "08"):
		return true
	case code == "57P01", code == "57P02", code == "57P03", code == "53300":
		return true
	case code == "55000":
		return strings.Contains(msg, "not currently accepting connections")
	}
	return false
}
