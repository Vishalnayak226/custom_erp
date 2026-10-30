package server

import (
	"net/url"
	"os"
)

// testConnStr mirrors engines/testdb_test.go's helper for this package - see
// the comment there for why the override exists. Default is unchanged: the
// portable Postgres on 5435 that environments.json/manage.ps1 assume.
//
// Stage 47.16.4b: as there, the tests' connections are a sanctioned purge
// (erp.sanctioned_write=purge), so a cleanup can delete the ledger rows a test
// posted; updating a posted row stays refused, as in production.
func testConnStr() string {
	conn := "postgres://postgres@localhost:5435/custom_erp?sslmode=disable"
	if v := os.Getenv("TEST_DATABASE_URL"); v != "" {
		conn = v
	}
	u, err := url.Parse(conn)
	if err != nil || u.Scheme == "" {
		return conn
	}
	q := u.Query()
	q.Set("erp.sanctioned_write", "purge")
	u.RawQuery = q.Encode()
	return u.String()
}
