package main

import (
	"database/sql"
	"os"
	"strings"
	"testing"

	_ "github.com/lib/pq"
)

// The tool is meant for production, so its session must refuse writes.
func TestReadOnlySessionRefusesWrites(t *testing.T) {
	raw := os.Getenv("TEST_DATABASE_URL")
	if raw == "" {
		raw = "postgres://postgres@localhost:5435/custom_erp?sslmode=disable"
	}
	conn, err := sql.Open("postgres", readOnly(raw))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.Ping(); err != nil {
		t.Skipf("no database: %v", err)
	}
	_, err = conn.Exec(`CREATE TEMP TABLE stateforecast_write_probe (x int)`)
	if err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("a write must fail in a read-only transaction, got %v", err)
	}
}

func TestSizeIsReadable(t *testing.T) {
	for n, want := range map[int64]string{512: "512 B", 2048: "2 KiB", 3 << 20: "3.0 MiB", 5 << 30: "5.0 GiB"} {
		if got := size(n); got != want {
			t.Errorf("size(%d) = %q, want %q", n, got, want)
		}
	}
}
