package trim

import (
	"strings"
	"unicode/utf8"

	"github.com/ethosagent/meterwise/internal/contract"
)

// Limits of schema_trim: a description longer than schemaMax is cut to at most schemaKeep bytes, at a sentence end or
// a space.
const (
	schemaMax  = 300
	schemaKeep = 220
)

// schemaTrim shortens an overlong tool description. It is lossy and it edits the tool definitions, which sit in front
// of the provider's cached prefix, so it runs only in aggressive mode, only when its toggle is on, and decides each
// description once per session like every other region.
type schemaTrim struct{}

func (schemaTrim) Name() contract.TrimStage { return contract.StageSchemaTrim }

func (schemaTrim) Apply(_ Unit, text string) (string, bool) {
	if len(text) <= schemaMax {
		return "", false
	}
	cut := schemaKeep
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	head := text[:cut]
	if i := strings.LastIndex(head, ". "); i > schemaKeep/2 {
		head = head[:i+1]
	} else if i := strings.LastIndexByte(head, ' '); i > schemaKeep/2 {
		head = head[:i]
	}
	return head + " …", true
}
