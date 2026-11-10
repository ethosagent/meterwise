package store

import (
	"testing"
	"time"
)

func TestCursorCodec(t *testing.T) {
	at := time.Date(2026, 3, 4, 5, 6, 7, 123456000, time.UTC)
	tests := []struct {
		name, id string
	}{
		{"session id", "sess-1"},
		{"id with a dot", "a.b.c"},
		{"numeric audit id", "42"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotAt, gotID, err := decodeCursor(encodeCursor(at, tc.id))
			if err != nil || !gotAt.Equal(at) || gotID != tc.id {
				t.Fatalf("round trip = %v %q %v, want %v %q", gotAt, gotID, err, at, tc.id)
			}
		})
	}
	for _, bad := range []string{"!!", "bm9kb3Q", encodeCursor(at, "")} {
		if _, _, err := decodeCursor(bad); err == nil {
			t.Errorf("decodeCursor(%q) accepted a malformed cursor", bad)
		}
	}
	if _, _, err := decodeCursor(""); err != nil {
		t.Errorf("empty cursor must be the first page, got %v", err)
	}
	if _, _, err := decodeAuditCursor(encodeCursor(at, "x")); err == nil {
		t.Error("audit cursor with a non-numeric id was accepted")
	}
	if _, id, err := decodeAuditCursor(encodeAuditCursor(at, 7)); err != nil || id != 7 {
		t.Errorf("audit round trip id = %d err = %v", id, err)
	}
}
