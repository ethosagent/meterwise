package store

import (
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

// A 55000 whose wording we do not recognise is permanent for the retry policy but counted, so a wording
// drift in a new Postgres shows on /statusz instead of silently turning an outage into data loss.
func TestUnexpectedStateErrorsAreCounted(t *testing.T) {
	before := UnexpectedStateErrors()
	if err := queryErr(&pgconn.PgError{Code: "55000", Message: "database is not currently accepting connections"}); !IsTransient(err) {
		t.Fatal("the known wording must stay transient")
	}
	if UnexpectedStateErrors() != before {
		t.Fatal("the known wording must not be counted")
	}
	err := queryErr(&pgconn.PgError{Code: "55000", Message: "database refuses connections for now"})
	if IsTransient(err) || UnexpectedStateErrors() != before+1 {
		t.Fatalf("unknown wording: transient=%v counted=%d", IsTransient(err), UnexpectedStateErrors()-before)
	}
	_ = queryErr(&pgconn.PgError{Code: "23505", Message: "dup"})
	if UnexpectedStateErrors() != before+1 {
		t.Fatal("only 55000 is counted")
	}
}
