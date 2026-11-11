package trim_test

import (
	"os"
	"runtime/debug"
	"testing"

	"github.com/ethosagent/meterwise/internal/nonet"
)

func TestMain(m *testing.M) { os.Exit(nonet.Run(m)) }

// raceOn reports whether the test binary is built with the race detector, which slows every call several times over.
func raceOn() bool {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return false
	}
	for _, s := range bi.Settings {
		if (s.Key == "-race" || s.Key == "-cover") && s.Value == "true" {
			return true
		}
	}
	return false
}
