package contract

// ClientFamilies lists the client families the Sessions filter offers (ruling S8), in display order. The
// proxy derives a family from any User-Agent (ledger.ClientFamily), so a client outside this list can still
// be stored; this is the set the dashboard names, and cmd/gen emits it to the web twin so the list has one
// source. A test in ledger keeps each entry producible by ClientFamily.
func ClientFamilies() []string {
	return []string{"opencode", "claude-code", "codex", "openai-python", "openai-node", "anthropic-python", "anthropic-node", "curl"}
}
