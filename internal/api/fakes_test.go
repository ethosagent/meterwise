package api_test

import (
	"context"
	"net/http"
	"sync"

	"github.com/ethosagent/meterwise/internal/access"
	"github.com/ethosagent/meterwise/internal/api"
	"github.com/ethosagent/meterwise/internal/contract"
)

type pinger struct{ err error }

func (p pinger) Ping(context.Context) error { return p.err }

type countingResolver struct {
	calls int
	err   error
}

func (c *countingResolver) Resolve(*http.Request) (contract.Session, error) {
	c.calls++
	if c.err != nil {
		return contract.Session{}, c.err
	}
	p := contract.Principal{UserID: "u-1", OrgID: "o-1", Role: contract.RoleAdmin}
	return contract.Session{User: contract.User{ID: "u-1"}, Member: true, Principal: p}, nil
}

// memTenancy is an in-memory Tenancy that only implements preferences; the other
// methods panic on the nil embedded interface (these tests never reach them).
type memTenancy struct {
	api.Tenancy
	mu            sync.Mutex
	byID          map[string]contract.Preferences
	gets, updates []string
}

func newMemTenancy() *memTenancy { return &memTenancy{byID: map[string]contract.Preferences{}} }

func (m *memTenancy) Profile(_ context.Context, id string) (contract.Profile, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gets = append(m.gets, id)
	if p, ok := m.byID[id]; ok {
		return contract.Profile{Preferences: p, ActiveWorkspaceID: "ws-1"}, nil
	}
	return contract.Profile{Preferences: api.DefaultPreferences(), ActiveWorkspaceID: "ws-1"}, nil
}

func (m *memTenancy) ListWorkspaces(context.Context, contract.Principal) ([]contract.Workspace, error) {
	return nil, nil
}

func (m *memTenancy) ListMembers(context.Context, contract.Principal) ([]contract.Member, error) {
	return nil, nil
}

func (m *memTenancy) ListInvitations(context.Context, contract.Principal) ([]contract.Invitation, error) {
	return nil, nil
}

func (m *memTenancy) UpdatePreferences(_ context.Context, id string, patch contract.PreferencesPatch) (contract.Preferences, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.updates = append(m.updates, id)
	p, ok := m.byID[id]
	if !ok {
		p = api.DefaultPreferences()
	}
	if patch.Theme != nil {
		p.Theme = *patch.Theme
	}
	if patch.ReducedMotion != nil {
		p.ReducedMotion = *patch.ReducedMotion
	}
	m.byID[id] = p
	return p, nil
}

// allowAuthz admits every caller; the gateway's decisions are tested against the real Authorizer
// in the stack tests.
type allowAuthz struct{}

func (allowAuthz) Authorize(context.Context, contract.Principal, contract.Permission, string) (access.Result, error) {
	return access.Result{Allowed: true, Reason: "admin"}, nil
}

func (allowAuthz) Filter(_ context.Context, _ contract.Principal, _ contract.Permission, ids []string) ([]string, error) {
	return ids, nil
}

func (allowAuthz) Snapshot(context.Context, contract.Principal) (contract.PermissionSnapshot, error) {
	return contract.PermissionSnapshot{Admin: true, Grants: []contract.Grant{}}, nil
}

// noAccess stands in for the access service: reads answer empty, writes panic on the nil
// embedded interface (the preference and route-table tests never write through it).
type noAccess struct{ api.Access }

func (noAccess) ListGroups(context.Context) ([]contract.Group, error)          { return nil, nil }
func (noAccess) Group(context.Context, string) (contract.Group, error)         { return contract.Group{}, nil }
func (noAccess) GroupGrants(context.Context, string) ([]contract.Grant, error) { return nil, nil }
func (noAccess) GroupMembers(context.Context, string) ([]contract.Member, error) {
	return nil, nil
}
func (noAccess) OrgDefaults(context.Context) ([]contract.Grant, error) { return nil, nil }
