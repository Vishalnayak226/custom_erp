package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestDecideMatrix(t *testing.T) {
	cases := []struct {
		host, build string
		allow       bool
	}{
		{"stable", "stable", true},
		{"stable", "candidate", false},
		{"candidate", "stable", true},
		{"candidate", "candidate", true},
		{"hold", "stable", false},
		{"hold", "candidate", false},
		{"", "stable", true},
		{"", "candidate", false},
		{"", "", true},
		{"Stable ", " STABLE", true},
		{"nightly", "stable", false},
		{"stable", "beta", false},
	}
	for _, c := range cases {
		if got := Decide(c.host, c.build); got.Allow != c.allow {
			t.Errorf("host=%q build=%q: allow=%v, want %v (%s)", c.host, c.build, got.Allow, c.allow, got.Reason)
		}
	}
}

func TestParseChannel(t *testing.T) {
	env := "JWT_SECRET=abc\n# UPGRADE_CHANNEL=hold\nexport UPGRADE_CHANNEL=\"Candidate\"\nOTHER=1\n"
	if v, ok := ParseChannel(env); !ok || v != "candidate" {
		t.Fatalf("got %q %v", v, ok)
	}
	if _, ok := ParseChannel("JWT_SECRET=x\n"); ok {
		t.Fatal("absent line reported present")
	}
	if v, _ := ParseChannel("UPGRADE_CHANNEL=stable\nUPGRADE_CHANNEL=hold\n"); v != "hold" {
		t.Fatalf("last line should win, got %q", v)
	}
}

func TestRunDryRunFromSampleEnv(t *testing.T) {
	run1 := func(env, build string) (int, string) {
		var out, errb bytes.Buffer
		code := run([]string{"-host-env", "-", "-build", build}, strings.NewReader(env), &out, &errb)
		return code, out.String()
	}
	if c, o := run1("UPGRADE_CHANNEL=stable\n", "candidate"); c != 1 || !strings.HasPrefix(o, "STOP") {
		t.Errorf("stable host + candidate build: %d %q", c, o)
	}
	if c, o := run1("UPGRADE_CHANNEL=hold\n", "stable"); c != 1 || !strings.Contains(o, "hold") {
		t.Errorf("hold host: %d %q", c, o)
	}
	if c, _ := run1("UPGRADE_CHANNEL=candidate\n", "candidate"); c != 0 {
		t.Errorf("candidate host + candidate build: %d", c)
	}
	var out, errb bytes.Buffer
	if c := run(nil, strings.NewReader(""), &out, &errb); c != 2 {
		t.Errorf("no args: %d", c)
	}
}
