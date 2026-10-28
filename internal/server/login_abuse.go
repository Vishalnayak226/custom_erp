package server

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// Stage 134.5 (49.2.5): login-abuse controls, on top of the per-address rate
// limiter (five sign-ins a minute) and the account lock
// (security.account_lockout_threshold / _duration_minutes).
//
//   - Backoff per name and address: after 3 failed sign-ins for one user name
//     from one address, the next try waits 2^(n-3) seconds, at most 15
//     minutes. A try inside the wait is refused without the password being
//     checked, so guessing slows to a crawl while a person who mistyped waits
//     a second or two. It is keyed by the name as typed, whether or not that
//     user exists, so the answer reveals nothing about the name.
//   - An unknown name costs the same bcrypt time as a real one, so timing
//     does not tell a valid name from an invalid one.
//   - The account lock cannot be used to keep a person out: a correct
//     password from a browser that person already proved with their
//     authenticator (Stage 125.8's remembered device) signs in through the
//     lock and clears it. Someone else's failed guesses cannot take that away.
//   - Detection: one address failing on 10 or more names within 15 minutes
//     (password spray), or one name failing from 10 or more addresses
//     (distributed guessing), is written to the audit log once per window.
//
// State is in memory, per server: lost on a restart, which only resets
// waits; the account lock itself is in the database.

const (
	loginBackoffFree   = 3
	loginBackoffMax    = 15 * time.Minute
	loginBackoffForget = time.Hour
	loginWatchWindow   = 15 * time.Minute
	loginWatchReport   = 10
	loginStateCap      = 50000
)

type loginBackoffEntry struct {
	failures int
	until    time.Time
	last     time.Time
}

type loginWatchEntry struct {
	start    time.Time
	seen     map[string]struct{}
	reported bool
}

var loginAbuse = struct {
	sync.Mutex
	backoff map[string]*loginBackoffEntry
	byAddr  map[string]*loginWatchEntry // address -> names failed
	byName  map[string]*loginWatchEntry // name -> addresses failed
}{backoff: map[string]*loginBackoffEntry{}, byAddr: map[string]*loginWatchEntry{}, byName: map[string]*loginWatchEntry{}}

// loginNameMax is users.username's width, VARCHAR(100): no longer name can
// belong to an account. Keying on the whole name let one failed sign-in keep
// a name of up to the request envelope's 1 MiB string in memory, three times
// over (49.10.5) - 50,000 of them before the cap below cleared anything.
const loginNameMax = 100

func loginNameKey(tenantID, username string) string {
	name := strings.TrimSpace(username)
	if len(name) > loginNameMax {
		name = name[:loginNameMax]
	}
	name = strings.ToLower(name)
	if len(name) > loginNameMax {
		name = name[:loginNameMax] // lower-casing can lengthen a few letters
	}
	return tenantID + "\x00" + name
}

// loginBackoffWait is how long this name and address must still wait.
func loginBackoffWait(tenantID, username, addr string) time.Duration {
	loginAbuse.Lock()
	defer loginAbuse.Unlock()
	e := loginAbuse.backoff[loginNameKey(tenantID, username)+"\x00"+addr]
	if e == nil {
		return 0
	}
	if wait := time.Until(e.until); wait > 0 {
		return wait
	}
	return 0
}

// loginFailed records a failed sign-in and reports whether it completed a
// spray or distributed-guessing pattern that should be logged now.
func loginFailed(tenantID, username, addr string) (spray, distributed bool) {
	now := time.Now()
	name := loginNameKey(tenantID, username)
	loginAbuse.Lock()
	defer loginAbuse.Unlock()
	if len(loginAbuse.backoff) > loginStateCap || len(loginAbuse.byAddr) > loginStateCap || len(loginAbuse.byName) > loginStateCap {
		pruneLoginAbuse(now)
	}
	key := name + "\x00" + addr
	e := loginAbuse.backoff[key]
	if e == nil || now.Sub(e.last) > loginBackoffForget {
		e = &loginBackoffEntry{}
		loginAbuse.backoff[key] = e
	}
	e.failures++
	e.last = now
	if e.failures >= loginBackoffFree {
		wait := loginBackoffMax
		if shift := e.failures - loginBackoffFree; shift < 10 {
			wait = min(time.Duration(1<<shift)*time.Second, loginBackoffMax)
		}
		e.until = now.Add(wait)
	}
	spray = loginWatch(loginAbuse.byAddr, tenantID+"\x00"+addr, name, now)
	distributed = loginWatch(loginAbuse.byName, name, addr, now)
	return spray, distributed
}

func loginWatch(m map[string]*loginWatchEntry, key, member string, now time.Time) bool {
	w := m[key]
	if w == nil || now.Sub(w.start) > loginWatchWindow {
		w = &loginWatchEntry{start: now, seen: map[string]struct{}{}}
		m[key] = w
	}
	if len(w.seen) < loginWatchReport*4 {
		w.seen[member] = struct{}{}
	}
	if !w.reported && len(w.seen) >= loginWatchReport {
		w.reported = true
		return true
	}
	return false
}

// loginSucceeded clears the backoff for this name and address.
func loginSucceeded(tenantID, username, addr string) {
	loginAbuse.Lock()
	delete(loginAbuse.backoff, loginNameKey(tenantID, username)+"\x00"+addr)
	loginAbuse.Unlock()
}

// pruneLoginAbuse drops what has expired; if an attack still fills the maps,
// they are emptied - losing waits is safer than growing without bound.
func pruneLoginAbuse(now time.Time) {
	for k, e := range loginAbuse.backoff {
		if now.Sub(e.last) > loginBackoffForget && now.After(e.until) {
			delete(loginAbuse.backoff, k)
		}
	}
	for _, m := range []map[string]*loginWatchEntry{loginAbuse.byAddr, loginAbuse.byName} {
		for k, w := range m {
			if now.Sub(w.start) > loginWatchWindow {
				delete(m, k)
			}
		}
	}
	if len(loginAbuse.backoff) > loginStateCap {
		loginAbuse.backoff = map[string]*loginBackoffEntry{}
	}
	if len(loginAbuse.byAddr) > loginStateCap {
		loginAbuse.byAddr = map[string]*loginWatchEntry{}
	}
	if len(loginAbuse.byName) > loginStateCap {
		loginAbuse.byName = map[string]*loginWatchEntry{}
	}
}

var (
	loginDummyHashOnce sync.Once
	loginDummyHash     []byte
)

// spendLoginHashTime runs one bcrypt comparison that can never match, so a
// sign-in for a name that does not exist takes as long as one that does.
func spendLoginHashTime(password string) {
	loginDummyHashOnce.Do(func() {
		loginDummyHash, _ = bcrypt.GenerateFromPassword([]byte("134.5 no such user"), bcrypt.DefaultCost)
	})
	_ = bcrypt.CompareHashAndPassword(loginDummyHash, []byte(password))
}

// loginWaitText is the wait as words: "2 seconds", "4 minutes".
func loginWaitText(d time.Duration) string {
	if s := int(d.Round(time.Second) / time.Second); s < 60 {
		if s <= 1 {
			return "1 second"
		}
		return fmt.Sprintf("%d seconds", s)
	}
	m := int((d + time.Minute - 1) / time.Minute)
	if m == 1 {
		return "1 minute"
	}
	return fmt.Sprintf("%d minutes", m)
}
