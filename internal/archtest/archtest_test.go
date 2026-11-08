package archtest

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestArchitecture(t *testing.T) {
	root := moduleRoot(t)
	violations := runOn(t, root)
	for _, v := range violations {
		t.Error(v.String())
	}
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		t.Fatalf("go env GOMOD: %v", err)
	}
	return filepath.Dir(strings.TrimSpace(string(out)))
}
