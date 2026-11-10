package tenancy

import (
	"encoding/json"

	"github.com/ethosagent/meterwise/internal/contract"
)

// KeepYourKeyNote is printed in the snippets: Meterwise never holds a provider key.
const KeepYourKeyNote = "keep using your own provider key; Meterwise forwards it untouched"

// RenderSnippets fills proxyURL (https://host/w/<id>, no trailing slash) into the five ready-to-paste
// client configurations. It is pure: the same URL gives the same text, with no I/O. The OpenAI wire
// expects a base ending in /v1; the Anthropic SDKs append /v1/messages themselves.
func RenderSnippets(proxyURL string) contract.SetupSnippets {
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

type opencodeFile struct {
	Schema   string                      `json:"$schema"`
	Provider map[string]opencodeProvider `json:"provider"`
}

type opencodeProvider struct {
	NPM     string                       `json:"npm"`
	Name    string                       `json:"name"`
	Options opencodeOptions              `json:"options"`
	Models  map[string]map[string]string `json:"models"`
}

type opencodeOptions struct {
	BaseURL string `json:"baseURL"`
	Auth    string `json:"apiKey"` // an {env:NAME} reference: the file holds no secret
}

// opencodeBlock is an opencode.json with one OpenAI-compatible provider pointing at the workspace
// URL; the key is read from the environment so the file holds no secret.
func opencodeBlock(proxyURL string) string {
	doc := opencodeFile{
		Schema: "https://opencode.ai/config.json",
		Provider: map[string]opencodeProvider{"meterwise": {
			NPM: "@ai-sdk/openai-compatible", Name: "Meterwise",
			Options: opencodeOptions{BaseURL: proxyURL + "/v1", Auth: "{env:OPENAI_API_KEY}"},
			Models:  map[string]map[string]string{"gpt-4o-mini": {"name": "gpt-4o-mini"}},
		}},
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil { // plain strings cannot fail to marshal
		return "{}"
	}
	return string(b) + "\n"
}
