package clerkmock_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/identity/clerkmock"
	"github.com/ethosagent/meterwise/internal/nonet"
)

func TestMain(m *testing.M) { os.Exit(nonet.Run(m)) }

func TestVerify(t *testing.T) {
	p, err := clerkmock.New()
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		bearer string
		want   contract.Code
		sub    string
	}{
		{"admin", "mock-admin-token", "", "user_admin_01"},
		{"member", "mock-member-token", "", "user_member_01"},
		{"expired", "mock-expired-token", contract.CodeUnauthorized, ""},
		{"unknown", "nope", contract.CodeUnauthorized, ""},
		{"empty", "", contract.CodeUnauthorized, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			id, err := p.Verify(context.Background(), tc.bearer)
			if tc.want == "" {
				if err != nil || id.Subject != tc.sub {
					t.Fatalf("got %+v, %v", id, err)
				}
				return
			}
			e := contract.AsError(err)
			if e.Code != tc.want {
				t.Fatalf("code %s, want %s", e.Code, tc.want)
			}
			if tc.bearer != "" && strings.Contains(e.Message, tc.bearer) {
				t.Fatal("error message echoes the token")
			}
		})
	}
}
