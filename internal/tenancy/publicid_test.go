package tenancy_test

import (
	"bytes"
	"errors"
	"regexp"
	"testing"

	"github.com/ethosagent/meterwise/internal/contract"
	"github.com/ethosagent/meterwise/internal/store"
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

func TestWithPublicIDRedrawsOnCollisionOnly(t *testing.T) {
	var tried []string
	err := tenancy.WithPublicID(nil, func(pid string) error {
		tried = append(tried, pid)
		if len(tried) < 3 {
			return store.ErrPublicIDTaken
		}
		return nil
	})
	if err != nil || len(tried) != 3 || tried[0] == tried[1] || tried[1] == tried[2] {
		t.Fatalf("err %v tried %v", err, tried)
	}

	boom := errors.New("boom")
	calls := 0
	if err := tenancy.WithPublicID(nil, func(string) error { calls++; return boom }); !errors.Is(err, boom) || calls != 1 {
		t.Fatalf("a non-collision error is returned at once: %v after %d calls", err, calls)
	}

	calls = 0
	err = tenancy.WithPublicID(nil, func(string) error { calls++; return store.ErrPublicIDTaken })
	var ce *contract.Error
	if !errors.As(err, &ce) || ce.Code != contract.CodeInternal || calls != 5 {
		t.Fatalf("exhaustion: %v after %d calls", err, calls)
	}

	if err := tenancy.WithPublicID(bytes.NewReader([]byte{1}), func(string) error { t.Fatal("try ran"); return nil }); err == nil {
		t.Fatal("a failing random source must fail")
	}
}
