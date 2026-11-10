package tenancy

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestNegativeCacheIsBoundedAndShortLived(t *testing.T) {
	n := newNegCache(8, 5*time.Second)
	boom := errors.New("not found")
	for i := range 50 {
		n.put(fmt.Sprint("id", i), boom, t0.Add(time.Duration(i)*time.Millisecond))
	}
	if n.size() != 8 {
		t.Fatalf("size %d, want the cap 8", n.size())
	}
	if _, ok := n.get("id49", t0.Add(time.Second)); !ok {
		t.Fatal("the newest unknown id was not remembered")
	}
	if _, ok := n.get("id49", t0.Add(6*time.Second)); ok {
		t.Fatal("an unknown id outlived its 5 s")
	}
	if got := n.sweep(t0.Add(time.Minute)); got != 8 || n.size() != 0 {
		t.Fatalf("sweep dropped %d, %d left", got, n.size())
	}
}
