package store

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/ethosagent/meterwise/internal/contract"
)

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// validID reports whether id can be cast to a uuid. A malformed id from a URL is
// a lookup that finds nothing, never a database error.
func validID(id string) bool { return uuidRe.MatchString(id) }

var errNotFound = contract.NewError(contract.CodeNotFound, "not found")

// uniqueViolation is the SQLSTATE of a unique constraint failure.
const uniqueViolation = "23505"

func isUnique(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == uniqueViolation
}

// queryErr turns a driver error into one that carries no connection detail:
// no rows is not_found, a server error keeps its SQLSTATE, anything else is a
// fixed "unreachable".
func queryErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, pgx.ErrNoRows):
		return errNotFound
	}
	var own *Error
	var typed *contract.Error
	if errors.As(err, &own) || errors.As(err, &typed) {
		return err
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		e := fail(ClassPool, "store: query failed (SQLSTATE "+pg.Code+")")
		e.transient = transientCode(pg.Code, pg.Message)
		return e
	}
	return fail(ClassUnreachable, "store: database unreachable")
}

func ts(v pgtype.Timestamptz) time.Time { return v.Time.UTC() }

// rfc3339 is the wire form of a stored timestamp.
func rfc3339(v pgtype.Timestamptz) string { return contract.RFC3339(ts(v)) }

func tsp(v pgtype.Timestamptz) *time.Time {
	if !v.Valid {
		return nil
	}
	t := v.Time.UTC()
	return &t
}

func pgts(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }

func settingsOf(raw []byte) map[string]any {
	out := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	return nonNilMap(out)
}

// isNotFound reports whether err, after scrubbing, is the not-found error.
func isNotFound(err error) bool { return errors.Is(queryErr(err), errNotFound) }

// clampInt32 narrows v into 0..MaxInt32 (a count or a page size, never negative).
func clampInt32(v int64) int32 {
	switch {
	case v > math.MaxInt32:
		return math.MaxInt32
	case v < 0:
		return 0
	}
	return int32(v)
}

// The keyset cursor of every paged list is "<unix micro>.<id>" in URL-safe base64: opaque to clients and
// exact for Postgres's microsecond timestamps. The id is text for sessions and a decimal number for the
// audit log (the audit list converts with strconv).
func encodeCursor(at time.Time, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("%d.%s", at.UnixMicro(), id)))
}

// decodeCursor is the inverse; an empty cursor is the first page (zero time, empty id). A malformed one
// is a 422 on the "cursor" field.
func decodeCursor(c string) (time.Time, string, error) {
	if c == "" {
		return time.Time{}, "", nil
	}
	bad := contract.Invalid("cursor", "is not a cursor this API issued")
	raw, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return time.Time{}, "", bad
	}
	us, id, ok := strings.Cut(string(raw), ".")
	micro, err := strconv.ParseInt(us, 10, 64)
	if !ok || err != nil || id == "" {
		return time.Time{}, "", bad
	}
	return time.UnixMicro(micro).UTC(), id, nil
}
