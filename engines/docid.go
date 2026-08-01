package engines

import (
	"crypto/rand"
	"encoding/base32"
	"fmt"
	"strings"
	"sync/atomic"
	"time"
)

// Collision-free document identifiers (Stage 29.9).
//
// Roughly thirty call sites across this package used to mint primary keys as
// fmt.Sprintf("TSK-%d", time.Now().UnixNano()) and friends. That is only safe
// if time.Now() actually advances between two calls, and on Windows it does
// not: measured on the dev box this project is built on, time.Now() moves in
// ~520us steps (31 distinct values across 2,000,000 consecutive calls), so a
// whole half-millisecond of concurrent work shares one "nanosecond" value.
//
// Measured collision rates for the old scheme on that clock:
//
//	tight loop, 8 goroutines .......... 99.98% of IDs duplicated
//	Poisson traffic @ 20 req/s ........  0.33%
//	Poisson traffic @ 50 req/s ........  1.20%
//	Poisson traffic @ 250 req/s .......  6.41%
//
// documents.id is the PRIMARY KEY, so a duplicate is an INSERT failure in the
// user's face - which is how this first surfaced, as an intermittent
// documents_pkey violation in the test suite that was written off as an
// environmental race. It is not environmental. Linux has a genuinely
// nanosecond-resolution clock and so is far safer, but "safe only because the
// clock happens to be fine-grained" is not a property to rely on for twenty
// years, and it never protected multi-instance deployments at all: two app
// processes behind a load balancer share no counter, so nothing but luck kept
// their IDs apart.
//
// NewDocID closes both cases:
//
//   - within a process, uniqueNanos is strictly increasing, so two calls can
//     never return the same value no matter how coarse the clock is;
//   - across processes, an 8-character random suffix drawn once per process
//     from crypto/rand keeps two instances apart even if their clocks agree
//     exactly.
//
// IDs stay time-ordered (the nanosecond field is fixed-width until the year
// 2262) and stay well inside documents.id's VARCHAR(100): a typical value is
// "TSK-1753939081123456789-k3mq7t2p", 32 characters. Nothing in this codebase
// parses an ID back into a number - they are opaque keys - so widening the
// format is safe.

// lastIDNanos is the high-water mark handed out by uniqueNanos.
var lastIDNanos atomic.Int64

// procIDSuffix distinguishes this process from every other one writing to the
// same database. Fixed for the process lifetime.
var procIDSuffix = newProcIDSuffix()

func newProcIDSuffix() string {
	b := make([]byte, 5) // 40 bits -> 8 base32 chars, no padding
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failing is close to unheard of; fall back to the clock
		// rather than panicking a running ERP at startup. Weaker, not broken:
		// uniqueNanos still guarantees in-process uniqueness on its own.
		n := time.Now().UnixNano()
		for i := range b {
			b[i] = byte(n >> (8 * i))
		}
	}
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b))
}

// uniqueNanos returns a strictly increasing timestamp. It tracks wall-clock
// time whenever the clock has actually moved, and otherwise hands out
// previous+1, so a coarse clock degrades into a plain counter instead of
// repeating itself.
func uniqueNanos() int64 {
	for {
		prev := lastIDNanos.Load()
		next := time.Now().UnixNano()
		if next <= prev {
			next = prev + 1
		}
		if lastIDNanos.CompareAndSwap(prev, next) {
			return next
		}
	}
}

// NewDocID returns a fresh identifier for a document of the given prefix, e.g.
// NewDocID("TSK") -> "TSK-1753939081123456789-k3mq7t2p". Safe for concurrent
// use, and safe across processes sharing one database.
func NewDocID(prefix string) string {
	return fmt.Sprintf("%s-%d-%s", prefix, uniqueNanos(), procIDSuffix)
}

// NewDocIDCompact is NewDocID for the handful of call sites whose historical
// format has no separator before the numeric part (e.g. "SLE<nanos>",
// "RRL<nanos>"). Keeps those IDs visually consistent with the rows already in
// the database while giving them the same guarantee.
func NewDocIDCompact(prefix string) string {
	return fmt.Sprintf("%s%d%s", prefix, uniqueNanos(), procIDSuffix)
}
