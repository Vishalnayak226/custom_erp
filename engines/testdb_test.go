package engines

import (
	"net/url"
	"os"
)

// testConnStr is the Postgres connection string every test in this package
// initializes db.DB with.
//
// The default is the repo's documented dev instance - the portable Postgres
// on port 5435 that environments.json and manage.ps1 both assume - so running
// `go test ./...` with no environment set behaves exactly as it always has.
// TEST_DATABASE_URL overrides it, which is the only way to run the suite
// against an instance that came up on a different port (a plain
// `pg_ctl start` with no `-o "-p 5435"` lands on Postgres's own 5432 default,
// and db.InitDB log.Fatalf's on a failed connection, so one unreachable port
// aborts the whole package binary rather than failing a single test).
//
// Stage 47.16.4b: the tests' connections are a sanctioned purge
// (gl_postings_guard.go), so a test's cleanup can still delete the ledger rows
// it posted. Updating a posted row stays refused, as in production; a test of
// the guard itself connects with testConnStrUnsanctioned.
func testConnStr() string {
	return withSanctionedPurge(testConnStrUnsanctioned())
}

// testConnStrUnsanctioned is the connection string as production has it.
func testConnStrUnsanctioned() string {
	if v := os.Getenv("TEST_DATABASE_URL"); v != "" {
		return v
	}
	return "postgres://postgres@localhost:5435/custom_erp?sslmode=disable"
}

// withSanctionedPurge adds the run-time setting to a postgres:// URL (lib/pq
// sends any unknown URL parameter as one).
func withSanctionedPurge(conn string) string {
	u, err := url.Parse(conn)
	if err != nil || u.Scheme == "" {
		return conn
	}
	q := u.Query()
	q.Set(sanctionedWriteSetting, sanctionPurge)
	u.RawQuery = q.Encode()
	return u.String()
}
