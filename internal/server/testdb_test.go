package server

import "os"

// testConnStr mirrors engines/testdb_test.go's helper for this package - see
// the comment there for why the override exists. Default is unchanged: the
// portable Postgres on 5435 that environments.json/manage.ps1 assume.
func testConnStr() string {
	if v := os.Getenv("TEST_DATABASE_URL"); v != "" {
		return v
	}
	return "postgres://postgres@localhost:5435/custom_erp?sslmode=disable"
}
