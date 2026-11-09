package tenancy_test

import (
	"bytes"
	"errors"
	"regexp"
	"testing"

	"github.com/ethosagent/meterwise/internal/tenancy"
)

func TestPublicIDsAre22Base62CharsAndUnique(t *testing.T) {
	re := regexp.MustCompile(`^[0-9A-Za-z]{22}$`)
	seen := map[string]bool{}
	for range 5000 {
		id, err := tenancy.NewPublicID(nil)
		if err != nil || !re.MatchString(id) {
			t.Fatalf("%q %v", id, err)
		}
		if seen[id] {
			t.Fatalf("duplicate id %q", id)
		}
		seen[id] = true
	}
}

func TestPublicIDDrawsAreUnbiased(t *testing.T) {
	// bytes at or above 248 (4*62) must be discarded, not folded onto the first characters
	src := bytes.NewReader(append(bytes.Repeat([]byte{255, 250, 248}, 44), bytes.Repeat([]byte{0, 61, 62}, 30)...))
	id, err := tenancy.NewPublicID(src)
	if err != nil {
		t.Fatal(err)
	}
	if id[:3] != "0z0" { // 0 -> '0', 61 -> 'z', 62 -> 62%62=0 -> '0'
		t.Fatalf("biased or wrong draw: %q", id)
	}
}

func TestPublicIDFailsWhenRandomnessFails(t *testing.T) {
	if _, err := tenancy.NewPublicID(bytes.NewReader([]byte{1, 2})); err == nil {
		t.Fatal("a short random source produced an id")
	}
	if _, err := tenancy.NewPublicID(errReader{}); err == nil {
		t.Fatal("a failing random source produced an id")
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("boom") }
