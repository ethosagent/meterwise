package clerkmock

import (
	"context"
	_ "embed" // fixtures are committed in testdata and embedded
	"encoding/json"
	"fmt"

	"github.com/ethosagent/meterwise/internal/contract"
)

//go:embed testdata/tokens.json
var fixtureJSON []byte

type fixture struct {
	Bearer  string `json:"bearer"`
	Subject string `json:"subject"`
	Email   string `json:"email"`
	Expired bool   `json:"expired"`
}

// Provider implements contract.IdentityProvider from fixtures.
type Provider struct {
	byBearer map[string]fixture
}

var _ contract.IdentityProvider = (*Provider)(nil)

// New loads the committed fixtures.
func New() (*Provider, error) {
	var f struct {
		Tokens []fixture `json:"tokens"`
	}
	if err := json.Unmarshal(fixtureJSON, &f); err != nil {
		return nil, fmt.Errorf("clerkmock: decode fixtures: %w", err)
	}
	p := &Provider{byBearer: make(map[string]fixture, len(f.Tokens))}
	for _, t := range f.Tokens {
		p.byBearer[t.Bearer] = t
	}
	return p, nil
}

// Verify returns the fixture identity, or CodeUnauthorized for an unknown or
// expired token. The message never echoes the presented token.
func (p *Provider) Verify(_ context.Context, bearer string) (contract.Identity, error) {
	t, ok := p.byBearer[bearer]
	if !ok || t.Expired {
		return contract.Identity{}, contract.NewError(contract.CodeUnauthorized, "invalid or expired token")
	}
	return contract.Identity{Subject: t.Subject, Email: t.Email}, nil
}
