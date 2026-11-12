package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// secretSpec is a key the env file must hold and how long its random value is
// (bytes, hex-encoded: the lengths deploy/erp.env.example asks for).
type secretSpec struct {
	Key   string
	Bytes int
}

var requiredSecrets = []secretSpec{
	{"JWT_SECRET", 48},
	{"BACKUP_ENCRYPTION_KEY", 32},
	{"CHANNEL_CREDENTIAL_KEY", 16},
	{"STORED_VALUE_KEY", 16},
	{"AUDIT_SIGNING_KEY", 16},
}

var validChannels = []string{"stable", "candidate", "hold"}

// envLine finds KEY= in text (uncommented). It returns the line index and the value.
func envValue(lines []string, key string) (int, string, bool) {
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "#") {
			continue
		}
		if k, v, ok := strings.Cut(t, "="); ok && strings.TrimSpace(k) == key {
			return i, strings.Trim(strings.TrimSpace(v), `"'`), true
		}
	}
	return -1, "", false
}

// isPlaceholder is true for a value nobody chose: empty, or the CHANGE_ME
// text deploy/erp.env.example ships.
func isPlaceholder(v string) bool {
	return v == "" || strings.Contains(v, "CHANGE_ME")
}

func readLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		out = append(out, sc.Text())
	}
	return out, sc.Err()
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// GenerateSecrets fills the secrets the env file lacks. It NEVER replaces a
// value somebody set: a key that holds a real value is left byte for byte as
// it was, and the file is rewritten only when something was missing. A missing
// file is created with mode 0600 (O_EXCL, so a file that appears meanwhile is
// not clobbered). It returns the keys it wrote and the keys it kept. Values
// are never returned or printed.
func GenerateSecrets(path string) (written, kept []string, err error) {
	lines, rerr := readLines(path)
	if rerr != nil && !os.IsNotExist(rerr) {
		return nil, nil, rerr
	}
	existed := rerr == nil
	changed := false
	for _, s := range requiredSecrets {
		i, v, ok := envValue(lines, s.Key)
		if ok && !isPlaceholder(v) {
			kept = append(kept, s.Key)
			continue
		}
		val, err := randomHex(s.Bytes)
		if err != nil {
			return nil, nil, err
		}
		if ok { // a placeholder line: replace in place, keeping its position
			lines[i] = s.Key + "=" + val
		} else {
			lines = append(lines, s.Key+"="+val)
		}
		written = append(written, s.Key)
		changed = true
	}
	if !changed {
		return nil, kept, nil
	}
	return written, kept, writeEnv(path, lines, existed)
}

// SetEnvIfAbsent records KEY=value only when the file has no real value for
// it. It returns whether it wrote, and the value already there.
func SetEnvIfAbsent(path, key, value string) (wrote bool, current string, err error) {
	lines, rerr := readLines(path)
	if rerr != nil && !os.IsNotExist(rerr) {
		return false, "", rerr
	}
	i, v, ok := envValue(lines, key)
	if ok && !isPlaceholder(v) {
		return false, v, nil
	}
	if ok {
		lines[i] = key + "=" + value
	} else {
		lines = append(lines, key+"="+value)
	}
	return true, "", writeEnv(path, lines, rerr == nil)
}

func writeEnv(path string, lines []string, existed bool) error {
	body := strings.Join(lines, "\n") + "\n"
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	if !existed {
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return err
		}
		if _, err := f.WriteString(body); err != nil {
			f.Close()
			return err
		}
		return f.Close()
	}
	mode := os.FileMode(0o600)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".env-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if err := tmp.Chmod(mode); err != nil && runtime.GOOS != "windows" {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if _, err := tmp.WriteString(body); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

func checkSecrets(c *Config) Result {
	r := Result{ID: "secrets", Name: "Secrets"}
	if c.EnvFile == "" {
		r.Status, r.Detail = Fail, "no -env file given"
		return r
	}
	if c.InitSecret {
		written, _, err := GenerateSecrets(c.EnvFile)
		if err != nil {
			r.Status, r.Detail = Fail, "could not write "+c.EnvFile+": "+err.Error()
			r.Next = "Make the folder writable by this user, or run with sudo."
			return r
		}
		if len(written) > 0 {
			r.Detail = "generated " + strings.Join(written, ", ") + " in " + c.EnvFile + ". Back that file up; lost keys cannot be recovered."
		}
	}
	lines, err := readLines(c.EnvFile)
	if err != nil {
		r.Status, r.Detail = Fail, c.EnvFile+" does not exist"
		r.Next = "Run again with -init-secrets to create it, then add DATABASE_URL (copy deploy/erp.env.example)."
		return r
	}
	var missing []string
	for _, s := range requiredSecrets {
		if _, v, ok := envValue(lines, s.Key); !ok || isPlaceholder(v) {
			missing = append(missing, s.Key)
		}
	}
	if len(missing) > 0 {
		r.Status, r.Detail = Fail, "not set: "+strings.Join(missing, ", ")
		r.Next = "Run again with -init-secrets. It only fills what is missing and never replaces a value you set."
		return r
	}
	if runtime.GOOS != "windows" {
		if st, err := os.Stat(c.EnvFile); err == nil && st.Mode().Perm()&0o077 != 0 {
			r.Status, r.Detail = Fail, fmt.Sprintf("%s is readable by other users (%04o)", c.EnvFile, st.Mode().Perm())
			r.Next = "chmod 600 " + c.EnvFile + " (and chown it to the erp user)."
			return r
		}
	}
	if r.Detail == "" {
		r.Detail = "all set, file not readable by others"
	}
	r.Status = Pass
	return r
}

func checkChannel(c *Config) Result {
	r := Result{ID: "channel", Name: "Upgrade channel"}
	ch := strings.ToLower(strings.TrimSpace(c.Channel))
	if c.EnvFile == "" {
		r.Status, r.Detail = Skip, "no env file"
		return r
	}
	lines, _ := readLines(c.EnvFile)
	_, cur, ok := envValue(lines, "UPGRADE_CHANNEL")
	cur = strings.ToLower(cur)
	valid := func(s string) bool {
		for _, v := range validChannels {
			if v == s {
				return true
			}
		}
		return false
	}
	switch {
	case ch != "" && !valid(ch):
		r.Status, r.Detail = Fail, fmt.Sprintf("%q is not one of %s", ch, strings.Join(validChannels, ", "))
	case ok && valid(cur) && (ch == "" || ch == cur):
		r.Status, r.Detail = Pass, "UPGRADE_CHANNEL="+cur
	case ok && valid(cur) && ch != cur:
		r.Status, r.Detail = Warn, "env file says "+cur+"; you asked for "+ch+" and it was not changed"
		r.Next = "Edit UPGRADE_CHANNEL in " + c.EnvFile + " by hand if you mean to switch."
	case ch == "":
		r.Status, r.Detail = Warn, "not chosen"
		r.Next = "Pass -channel stable (everyone), candidate (early) or hold (only by hand). Recorded in the env file."
	default:
		if _, _, err := SetEnvIfAbsent(c.EnvFile, "UPGRADE_CHANNEL", ch); err != nil {
			r.Status, r.Detail = Fail, "could not record it: "+err.Error()
			return r
		}
		r.Status, r.Detail = Pass, "recorded UPGRADE_CHANNEL="+ch
	}
	if r.Status == Pass {
		r.Detail += ". The Deploy click reads it (gate D0d)."
	}
	return r
}
