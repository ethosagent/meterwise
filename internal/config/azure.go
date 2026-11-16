package config

import (
	"regexp"
	"strings"
)

// EnvAzureResourceName names the Azure OpenAI resource the proxy's "azure" provider forwards to
// (https://<name>.openai.azure.com/openai). Unset, the provider does not exist (ADR 0034).
const EnvAzureResourceName = "AZURE_RESOURCE_NAME"

// azureResource is Azure's rule for a resource (custom subdomain) name: 2-64 letters, digits and
// hyphens, starting and ending with a letter or digit.
var azureResource = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,62}[A-Za-z0-9]$`)

func loadAzure(get Lookup, p *problems) string {
	name := strings.TrimSpace(str(get, EnvAzureResourceName, ""))
	if name != "" && !azureResource.MatchString(name) {
		p.add(EnvAzureResourceName, "must be the resource name only, 2-64 letters, digits or hyphens (example my-resource, not my-resource.openai.azure.com)")
		return ""
	}
	return name
}
