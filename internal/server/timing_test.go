package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"custom_erp/db"
	"custom_erp/engines"

	"golang.org/x/crypto/bcrypt"
)

// Stage 49.10.3 (timing): an answer that would tell a caller something exists
// must take about as long as the one that says it does not. Measured, with
// the two cases interleaved so a slow moment hits both.

func stage49103Median(d []time.Duration) time.Duration {
	sort.Slice(d, func(i, j int) bool { return d[i] < d[j] })
	return d[len(d)/2]
}

// stage49103Similar: the medians are within a factor of 3 or 40 ms of each
// other - wide enough for a shared test machine, narrow enough to catch a
// branch that does a bcrypt, a mail send or a second query chain.
func stage49103Similar(a, b time.Duration) bool {
	lo, hi := a, b
	if lo > hi {
		lo, hi = hi, lo
	}
	return hi-lo < 40*time.Millisecond || hi < 3*lo
}

func TestStage49103TimingDoesNotRevealExistence(t *testing.T) {
	db.InitDB(testConnStr())
	stamp := time.Now().Format("150405")
	a := provisionHarnessClient(t, "tim_a_"+stamp)
	b := provisionHarnessClient(t, "tim_b_"+stamp)
	seedHarnessRecords(t, a, harnessMarkerA, false)
	seedHarnessRecords(t, b, harnessMarkerB, true)

	previous := http.DefaultServeMux
	http.DefaultServeMux = http.NewServeMux()
	registerRoutes()
	mux := http.DefaultServeMux
	t.Cleanup(func() { http.DefaultServeMux = previous })

	hash, _ := bcrypt.GenerateFromPassword([]byte("Right-Pass-49103"), bcrypt.DefaultCost)
	if _, err := db.DB.Exec(`INSERT INTO `+a.schema+`.users (id, username, password_hash, email, role, status, location_code)
		VALUES ('tim-admin', 'tim-admin', 'x', 'tim-admin@timing.invalid', 'Super Admin', 'Active', 'HO'),
		       ('tim-cashier', 'tim-cashier', $1, 'tim-cashier@timing.invalid', 'Cashier', 'Active', 'HO')`, string(hash)); err != nil {
		t.Fatal(err)
	}
	engines.ResetLiveUserStateCache()
	token := engines.SignToken("tim-admin", "tim-admin", engines.RoleSuperAdmin, a.tenant, "HO", 1)

	seq := 0
	do := func(method, path, tok string, body interface{}) (*httptest.ResponseRecorder, time.Duration) {
		seq++
		raw, _ := json.Marshal(body)
		req := httptest.NewRequest(method, path, bytes.NewReader(raw))
		if tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		} else {
			req.Header.Set("X-Tenant-ID", a.tenant)
		}
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = fmt.Sprintf("198.18.%d.%d:4000", seq/250, seq%250+1)
		rec := httptest.NewRecorder()
		start := time.Now()
		mux.ServeHTTP(rec, req)
		return rec, time.Since(start)
	}
	measure := func(name string, n int, hidden, absent func() (int, time.Duration)) {
		t.Helper()
		var dh, da []time.Duration
		var ch, ca int
		for i := 0; i < n; i++ {
			c, d := hidden()
			ch = c
			dh = append(dh, d)
			c, d = absent()
			ca = c
			da = append(da, d)
		}
		mh, ma := stage49103Median(dh), stage49103Median(da)
		t.Logf("%s: existing-but-refused %v (status %d), absent %v (status %d)", name, mh, ch, ma, ca)
		if ch != ca {
			t.Errorf("%s: statuses differ (%d vs %d)", name, ch, ca)
		}
		if !stage49103Similar(mh, ma) {
			t.Errorf("%s: timing reveals existence - %v vs %v", name, mh, ma)
		}
	}

	// A record only B holds, against one nobody holds.
	measure("record lookup", 25, func() (int, time.Duration) {
		r, d := do("GET", "/api/v1/doc/Item/"+harnessOnlyInB, token, nil)
		return r.Code, d
	}, func() (int, time.Duration) {
		r, d := do("GET", "/api/v1/doc/Item/HARN-NOBODY-HAS-THIS", token, nil)
		return r.Code, d
	})

	// Sign-in: a real user with the wrong password, against no such user.
	measure("sign-in", 6, func() (int, time.Duration) {
		r, d := do("POST", "/api/v1/login", "", map[string]string{"username": "tim-cashier", "password": "Wrong-Pass-1"})
		return r.Code, d
	}, func() (int, time.Duration) {
		r, d := do("POST", "/api/v1/login", "", map[string]string{"username": "tim-nobody", "password": "Wrong-Pass-1"})
		return r.Code, d
	})

	// Password reset with a slow mail relay: the mail must not be sent on the
	// caller's time, or the reply is slow only for an account that exists.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { time.Sleep(1500 * time.Millisecond); c.Close() }()
		}
	}()
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	t.Setenv("SMTP_HOST", "127.0.0.1")
	t.Setenv("SMTP_PORT", port)
	t.Setenv("ERP_ENABLE_EXTERNAL_SIDE_EFFECTS", "1")
	measure("password reset", 2, func() (int, time.Duration) {
		r, d := do("POST", "/api/v1/auth/forgot-password", "", map[string]string{"username_or_email": "tim-cashier"})
		return r.Code, d
	}, func() (int, time.Duration) {
		r, d := do("POST", "/api/v1/auth/forgot-password", "", map[string]string{"username_or_email": "tim-nobody"})
		return r.Code, d
	})
}
