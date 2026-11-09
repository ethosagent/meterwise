// Command clerk-replay signs the committed Clerk webhook fixtures with a Svix
// secret and posts them to a running meterwised, so the webhook path is
// exercised with signature verification ON (no tunnel, no Clerk account):
//
//	CLERK_WEBHOOK_SECRET=whsec_... go run ./cmd/clerk-replay -fixture user.created
//	go run ./cmd/clerk-replay -fixture all -url http://127.0.0.1:8081/api/webhooks/clerk
//
// Each run uses fresh message ids and the current timestamp, so it is not a
// replay in the security sense; sending the same capture twice is rejected.
package main
