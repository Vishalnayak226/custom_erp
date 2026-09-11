package main

import (
	"net"
	"testing"
)

func TestExternalLinksCannotTargetPrivateServicesOrCredentials(t *testing.T) {
	for _, raw := range []string{"http://localhost/x", "http://127.0.0.1", "http://[::1]", "http://169.254.169.254/latest", "http://10.1.1.1", "https://user:password@example.com", "https://example.com/?token=private", "https://example.com:5435", "file:///etc/passwd"} {
		if _, err := publicLink(raw); err == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	u, err := publicLink("https://example.com/manual#help")
	if err != nil || u.Fragment != "" {
		t.Fatal(u, err)
	}
	for _, address := range []string{"100.100.100.200", "198.18.1.1", "fc00::1", "::ffff:192.168.1.1"} {
		if publicIP(net.ParseIP(address)) {
			t.Errorf("accepted nonpublic %s", address)
		}
	}
}

func TestNonportableRuleIgnoresLiteralPolicyExample(t *testing.T) {
	if nonportable.MatchString("Do not use `file:///` links.") {
		t.Fatal("policy explanation mistaken for actual link")
	}
	if !nonportable.MatchString("[source](file:///c:/work/file.md)") {
		t.Fatal("actual local file URI ignored")
	}
}
