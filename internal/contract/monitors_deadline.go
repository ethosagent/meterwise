package contract

import (
	"context"
	"time"
)

// NearDeadline reports whether ctx has a deadline nearer than min at now. history and monitors share it
// for their Before's margin: a chain about to give up gets no optional work (they may not import each
// other, ruling R1). A context without a deadline is never near one.
func NearDeadline(ctx context.Context, now time.Time, min time.Duration) bool {
	d, ok := ctx.Deadline()
	return ok && d.Sub(now) < min
}
