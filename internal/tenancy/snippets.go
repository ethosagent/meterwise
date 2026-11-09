package tenancy

import (
	"encoding/json"

	"github.com/ethosagent/meterwise/internal/contract"
)

// KeepYourKeyNote is printed in the snippets: Meterwise never holds a provider key.
const KeepYourKeyNote = "keep using your own provider key; Meterwise forwards it untouched"

// SnippetRenderer renders the ready-to-paste client configurations for a workspace URL. It is pure:
// the same URL gives the same text, with no I/O.
type SnippetRenderer struct{}

// Render fills proxyURL (https://host/w/<id>, no trailing slash) into the five snippets. The
// OpenAI wire expects a base ending in /v1; the Anthropic SDKs append /v1/messages themselves.
func (SnippetRenderer) Render(proxyURL string) contract.SetupSnippets {
	return contract.SetupSnippets{
		OpenaiEnv:     "# " + KeepYourKeyNote + "\nexport OPENAI_BASE_URL=" + proxyURL + "/v1\nexport OPENAI_API_KEY=<your OpenAI key>\n",
		AnthropicEnv:  "# " + KeepYourKeyNote + "\nexport ANTHROPIC_BASE_URL=" + proxyURL + "\nexport ANTHROPIC_API_KEY=<your Anthropic key>\n",
		OpencodeJSON:  opencodeBlock(proxyURL),
		ClaudeCodeEnv: "# " + KeepYourKeyNote + " (API-key mode)\nexport ANTHROPIC_BASE_URL=" + proxyURL + "\nexport ANTHROPIC_API_KEY=<your Anthropic key>\nclaude\n",
		Curl: "# " + KeepYourKeyNote + "\ncurl " + proxyURL + "/v1/chat/completions \\\n" +
			"  -H \"Authorization: Bearer $OPENAI_API_KEY\" \\\n" +
			"  -H \"Content-Type: application/json\" \\\n" +
			"  -d '{\"model\":\"gpt-4o-mini\",\"messages\":[{\"role\":\"user\",\"content\":\"ping\"}]}'\n",
	}
}

// opencodeBlock is an opencode.json with one OpenAI-compatible provider pointing at the workspace
// URL; the key is read from the environment so the file holds no secret.
func opencodeBlock(proxyURL string) string {
	doc := map[string]any{
		"$schema": "https://opencode.ai/config.json",
		"provider": map[string]any{
			"meterwise": map[string]any{
				"npm":  "@ai-sdk/openai-compatible",
				"name": "Meterwise",
				"options": map[string]any{
					"baseURL": proxyURL + "/v1",
					"apiKey":  "{env:OPENAI_API_KEY}",
				},
				"models": map[string]any{"gpt-4o-mini": map[string]any{"name": "gpt-4o-mini"}},
			},
		},
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil { // a map of strings cannot fail to marshal
		return "{}"
	}
	return string(b) + "\n"
}
