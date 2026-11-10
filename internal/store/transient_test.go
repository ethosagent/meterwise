package store_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store"
)

func TestIsTransient(t *testing.T) {
	pg := func(code, msg string) error {
		return fmt.Errorf("wrapped: %w", &pgconn.PgError{Code: code, Message: msg})
	}
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"unreachable store error", &store.Error{Class: store.ClassUnreachable}, true},
		{"pool store error", &store.Error{Class: store.ClassPool}, false},
		{"dial error", fmt.Errorf("x: %w", &net.OpError{Op: "dial", Err: errors.New("connection refused")}), true},
		{"context deadline on connect", fmt.Errorf("x: %w", context.DeadlineExceeded), true},
		{"plain error", errors.New("boom"), false},
		{"contract error", contract.Invalid("f", "bad"), false},
		{"08006 connection failure", pg("08006", "connection failure"), true},
		{"08001 cannot establish", pg("08001", "x"), true},
		{"57P01 admin shutdown", pg("57P01", "terminating connection due to administrator command"), true},
		{"57P02 crash shutdown", pg("57P02", "x"), true},
		{"57P03 cannot connect now", pg("57P03", "the database system is starting up"), true},
		{"53300 too many connections", pg("53300", "sorry, too many clients already"), true},
		{"55000 not accepting", pg("55000", `database "d" is not currently accepting connections`), true},
		{"55000 other", pg("55000", "cannot execute in a read-only transaction"), false},
		{"23505 unique violation", pg("23505", "duplicate key"), false},
		{"22P02 bad value", pg("22P02", "invalid input syntax"), false},
		{"42P01 undefined table", pg("42P01", "relation does not exist"), false},
	}
	for _, tc := range tests {
		if got := store.IsTransient(tc.err); got != tc.want {
			t.Errorf("%s: IsTransient = %v, want %v", tc.name, got, tc.want)
		}
	}
}
