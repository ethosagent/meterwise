package main

import "os"

// version and commit are stamped by the release build (-ldflags "-X main.version=...").
var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	os.Exit(run(os.Args[1:], os.LookupEnv, os.Stdout, os.Stderr))
}
