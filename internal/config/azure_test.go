package config_test

import (
	"strings"
	"testing"

	"github.com/ethosagent/meterwise/internal/config"
	"github.com/ethosagent/meterwise/internal/contract"
)

func TestAzureResourceName(t *testing.T) {
	cases := []struct {
		name, in, want string
		bad            bool
	}{
		{name: "unset leaves azure off", in: "", want: ""},
		{name: "a plain name is kept", in: "rudder-ai-mitesh", want: "rudder-ai-mitesh"},
		{name: "surrounding space is trimmed", in: "  my-res1 ", want: "my-res1"},
		{name: "a full host is refused", in: "my-res.openai.azure.com", bad: true},
		{name: "a url is refused", in: "https://my-res", bad: true},
		{name: "a leading hyphen is refused", in: "-my-res", bad: true},
		{name: "a trailing hyphen is refused", in: "my-res-", bad: true},
		{name: "one character is refused", in: "a", bad: true},
		{name: "over 64 characters is refused", in: strings.Repeat("a", 65), bad: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kv := map[string]string{config.EnvDatabaseURL: goodDB}
			if tc.in != "" {
				kv[config.EnvAzureResourceName] = tc.in
			}
			c, err := config.Load(env(kv))
			if tc.bad {
				e := contract.AsError(err)
				if e == nil || e.Code != contract.CodeConfigInvalid || !strings.Contains(strings.Join(e.Details, "\n"), config.EnvAzureResourceName) {
					t.Fatalf("want a %s problem, got %v", config.EnvAzureResourceName, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if c.AzureResourceName != tc.want {
				t.Fatalf("AzureResourceName = %q, want %q", c.AzureResourceName, tc.want)
			}
			if got := c.Redacted()[config.EnvAzureResourceName]; got != tc.want {
				t.Fatalf("Redacted shows %q, want %q", got, tc.want)
			}
		})
	}
}
