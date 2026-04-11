module petboard

go 1.22

// modernc.org/sqlite is the pure-Go SQLite driver — no CGO, so the
// binary stays self-contained on the VM without needing build-essential
// in the production image. Indirect dependencies are populated by
// `go mod tidy` at install time; this file only pins the direct one.
require modernc.org/sqlite v1.34.1
