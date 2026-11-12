package main

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"regexp"
	"strings"
)

// The safety rules. The tool posts real documents, so it refuses anything that
// is not plainly a scratch target: a non-local host, a production environment,
// external side effects switched on, or a database name that does not say it is
// a scratch one.

var scratchDBName = regexp.MustCompile(`(?i)(test|scratch|synth|bench|perf|sandbox|tmp|journey|soak)`)

// CheckBaseURL accepts only a localhost target unless allowRemote is set.
func CheckBaseURL(raw string, allowRemote bool) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("refused: %q is not a usable base URL", raw)
	}
	if allowRemote {
		return nil
	}
	if !isLocalHost(u.Hostname()) {
		return fmt.Errorf("refused: host %q is not local - this tool posts real documents; pass -allow-remote only for a scratch server you own", u.Hostname())
	}
	return nil
}

func isLocalHost(h string) bool {
	h = strings.ToLower(strings.Trim(h, "[]"))
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// DBNameFromURL returns the database name of a postgres URL.
func DBNameFromURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(u.Path, "/")
}

// CheckScratchDB refuses a database whose name does not look like a scratch
// one, unless allow names it exactly. Its host must be local too.
func CheckScratchDB(dbURL, allow string, allowRemote bool) error {
	u, err := url.Parse(dbURL)
	if err != nil || u.Host == "" {
		return errors.New("refused: DATABASE_URL is missing or unreadable")
	}
	if !allowRemote && !isLocalHost(u.Hostname()) {
		return fmt.Errorf("refused: database host %q is not local", u.Hostname())
	}
	name := DBNameFromURL(dbURL)
	if name == "" {
		return errors.New("refused: DATABASE_URL names no database")
	}
	if allow != "" && allow == name {
		return nil
	}
	if scratchDBName.MatchString(name) {
		return nil
	}
	return fmt.Errorf("refused: database %q does not look like a scratch database - use a name with test/scratch/synth/bench/perf/journey/soak in it, or pass -allow-db %s to name this one on purpose", name, name)
}

// CheckEnvironment refuses a production environment and live side effects.
func CheckEnvironment(getenv func(string) string) error {
	if strings.EqualFold(strings.TrimSpace(getenv("ENV")), "production") || strings.EqualFold(strings.TrimSpace(getenv("APP_ENV")), "production") {
		return errors.New("refused: ENV=production")
	}
	if v := strings.TrimSpace(getenv("ERP_DISABLE_EXTERNAL_SIDE_EFFECTS")); v != "1" && !strings.EqualFold(v, "true") {
		return errors.New("refused: set ERP_DISABLE_EXTERNAL_SIDE_EFFECTS=1 so no journey can reach a payment gateway, courier or marketplace")
	}
	return nil
}

// CheckAll runs every guard for a journey or soak run.
func CheckAll(baseURL, dbURL, allowDB string, allowRemote bool) error {
	if err := CheckEnvironment(os.Getenv); err != nil {
		return err
	}
	if err := CheckBaseURL(baseURL, allowRemote); err != nil {
		return err
	}
	return CheckScratchDB(dbURL, allowDB, allowRemote)
}
