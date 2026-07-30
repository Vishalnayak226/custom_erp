package engines

import (
	"custom_erp/db"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"
)

// Live user-state re-check (Stage 29.8).
//
// The gap this closes: ParseToken is pure HMAC + parsing and never touches the
// database, so every claim in a token is frozen at the moment it was issued.
// Deactivating a user blocked new logins (handlers_auth.go's login query
// filters `status = 'Active'`) but did nothing to the token they were already
// holding - a dismissed employee kept full access for the remainder of
// JWT_EXPIRY_HOURS, up to 24h by default. The same staleness applied to a role
// change: demoting someone from HR/Admin left their live token still asserting
// role=HR/Admin, so the demotion silently didn't take effect until they next
// logged in.
//
// Why this rather than a jti revocation list: a denylist can only revoke whole
// tokens, so it addresses deactivation but not the role half at all (the
// revoked user would simply log back in, and a demoted user's token was never
// revoked in the first place). Re-reading the user's current row fixes both,
// needs no new table, no logout endpoint and no expiry sweeper, and stays
// inside this repo's no-new-infrastructure preference. The cost is one indexed
// primary-key read per user per cache window.
//
// Cache: without one this would be a database round trip on every authenticated
// request. AUTH_STATE_CACHE_SECONDS (default 30) bounds how long a
// deactivation can take to be felt. 30s is deliberately short - this is a
// security control, and the read is a PK hit on a small table.

// errUserNotActive is returned when the token is cryptographically valid but
// the account behind it is gone, deleted, or no longer Active.
var errUserNotActive = errors.New("user account is not active")

const defaultAuthStateCacheTTL = 30 * time.Second

// authStateCacheMaxEntries bounds memory if a tenant has a very large number of
// distinct active users. On overflow the whole map is dropped rather than
// evicted entry by entry: this is a short-TTL cache, so a rare full flush costs
// one extra query per active user and keeps the code free of an LRU.
const authStateCacheMaxEntries = 10000

func authStateCacheTTL() time.Duration {
	if v := os.Getenv("AUTH_STATE_CACHE_SECONDS"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
			return time.Duration(secs) * time.Second
		}
	}
	return defaultAuthStateCacheTTL
}

// LiveUserState is the authoritative, current server-side view of a user,
// as opposed to whatever their token claimed when it was minted.
type LiveUserState struct {
	Role         string
	LocationCode string
}

type authStateEntry struct {
	state     LiveUserState
	fetchedAt time.Time
}

var (
	authStateMu    sync.RWMutex
	authStateCache = map[string]authStateEntry{}
)

// ResolveLiveUserState returns the user's current role and location, or
// errUserNotActive if the account no longer exists or is not Active. Callers
// must treat a non-nil error as "reject this request".
func ResolveLiveUserState(tenantID, userID string) (LiveUserState, error) {
	if userID == "" {
		return LiveUserState{}, errUserNotActive
	}
	key := tenantID + "|" + userID
	ttl := authStateCacheTTL()

	if ttl > 0 {
		authStateMu.RLock()
		entry, ok := authStateCache[key]
		authStateMu.RUnlock()
		if ok && time.Since(entry.fetchedAt) < ttl {
			// A cached negative result is represented by an empty Role, so a
			// deactivated user isn't re-queried on every request either.
			if entry.state.Role == "" {
				return LiveUserState{}, errUserNotActive
			}
			return entry.state, nil
		}
	}

	schema, err := db.GetTenantSchema(tenantID)
	if err != nil {
		return LiveUserState{}, err
	}

	var state LiveUserState
	err = db.DB.QueryRow(fmt.Sprintf(
		`SELECT role, location_code FROM %s.users WHERE id = $1 AND status = 'Active'`, schema),
		userID).Scan(&state.Role, &state.LocationCode)

	switch {
	case err == sql.ErrNoRows:
		cacheAuthState(key, LiveUserState{}, ttl)
		return LiveUserState{}, errUserNotActive
	case err != nil:
		// A genuine database failure must NOT be cached and must not be
		// mistaken for "this user is deactivated" - the caller distinguishes
		// this from errUserNotActive and fails with a retryable 503 rather
		// than logging a legitimate user out.
		return LiveUserState{}, err
	}

	// A user row with an empty role would collide with the negative-cache
	// marker above, so normalise it to the same rejection rather than caching
	// an ambiguous entry.
	if state.Role == "" {
		cacheAuthState(key, LiveUserState{}, ttl)
		return LiveUserState{}, errUserNotActive
	}

	cacheAuthState(key, state, ttl)
	return state, nil
}

func cacheAuthState(key string, state LiveUserState, ttl time.Duration) {
	if ttl <= 0 {
		return
	}
	authStateMu.Lock()
	if len(authStateCache) >= authStateCacheMaxEntries {
		authStateCache = map[string]authStateEntry{}
	}
	authStateCache[key] = authStateEntry{state: state, fetchedAt: time.Now()}
	authStateMu.Unlock()
}

// IsUserNotActiveError reports whether err is the "account gone/deactivated"
// case, as opposed to a database failure. internal/server's middleware uses
// this to pick 401 vs 503.
func IsUserNotActiveError(err error) bool { return errors.Is(err, errUserNotActive) }

// InvalidateLiveUserState drops a user's cached state so a deactivation or
// role change takes effect on the very next request instead of waiting out the
// cache window. Called from the paths that change either field; safe to call
// for a user that was never cached.
func InvalidateLiveUserState(tenantID, userID string) {
	authStateMu.Lock()
	delete(authStateCache, tenantID+"|"+userID)
	authStateMu.Unlock()
}

// ResetLiveUserStateCache clears the whole cache. Test-support only - the
// engines tests flip a user's status directly in SQL, which this package
// cannot observe.
func ResetLiveUserStateCache() {
	authStateMu.Lock()
	authStateCache = map[string]authStateEntry{}
	authStateMu.Unlock()
}
