// Package history is the per-session call ring (Plan 06). Its Hook is first in the chain: Before
// stamps the call's Meta with a read-only contract.RingView and the *contract.CallVerdicts the
// monitors fill (rulings R2 and R3), After appends the finished call, OnSessionClosed evicts. The
// ring holds hashes, tool names, counts and token numbers only, never message text (R7), and is
// rebuilt from Call.Messages when a session has none (restart safe). The package is pure (rule H3):
// standard library, tollgate's root and internal/contract.
package history
