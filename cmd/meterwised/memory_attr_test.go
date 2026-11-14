package main

import (
	"testing"

	"github.com/ethosagent/meterwise/internal/memory"
	"github.com/ethosagent/meterwise/internal/wsauth"
)

// memory reads its settings from the tenant attribute wsauth writes; the two names must stay equal, or memory would
// silently fall back to its defaults (mode off).
func TestMemoryAndWsauthAgreeOnTheTenantAttributeName(t *testing.T) {
	if memory.AttrMemory != wsauth.AttrMemory {
		t.Fatalf("memory.AttrMemory %q != wsauth.AttrMemory %q", memory.AttrMemory, wsauth.AttrMemory)
	}
}
