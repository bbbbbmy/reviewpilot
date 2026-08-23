package webdashboard

import "os"

// osGetenv indirection so tests can mock env vars without touching
// the production path.
var osGetenv = os.Getenv
