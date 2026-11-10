// Package monitors is the Plan 06 waste detector: six pure detectors over the per-session history
// ring and the live stream, and the tollgate Hook that acts on them (observe, note or block per
// workspace). The Hook is second in the chain. It reads the contract.RingView and writes the
// verdicts through the *contract.CallVerdicts that history put in Meta (rulings R2 and R3); it never
// imports history or savings (R1, rule H3), meeting them only through contract types.
//
// Before does no I/O and no clock reads beyond the deadline check, and skips itself when the chain
// deadline is near. Stream detectors (reasoning_loop, second_guessing) read text collected by
// OnEvent, off the client path, and decide in After. Provider JSON is edited only by the wire
// subpackage (R4).
package monitors
