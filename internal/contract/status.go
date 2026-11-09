package contract

// StatusSource is a component that reports its own counters for /statusz on the admin listener
// (queue depth, drops, last error). Status must be cheap, safe for concurrent use and carry no
// secret, no message text and no public workspace id. The values must marshal as JSON.
type StatusSource interface {
	Status() map[string]any
}
