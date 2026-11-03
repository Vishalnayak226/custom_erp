package db

import (
	"bytes"
	"runtime"
	"strconv"
	"sync"
	"time"
)

// Stage 49.13.3a: an optional database statement timeout, applied where every
// tenant transaction already passes - SetSearchPath. It is SET LOCAL, so it
// ends with the transaction and never leaks onto a pooled connection.
// Migrations, backups (pg_dump) and the migration runner do not go through
// SetSearchPath and are never subject to it.
//
// StatementTimeoutFor is set by the engines package (it reads the
// platform.db_statement_timeout_seconds setting for the schema); nil or a
// zero answer means no timeout, the default.
var StatementTimeoutFor func(schema string) time.Duration

// exempt holds the goroutines that run under their own, longer bound (a
// report run): their transactions get that bound instead.
var exempt sync.Map // goroutine id -> time.Duration

// ExemptStatementTimeout gives the calling goroutine its own bound for as
// long as the returned release has not been called. A goroutine id is read
// from the stack header only when a timeout is in force.
func ExemptStatementTimeout(bound time.Duration) (release func()) {
	id := goroutineID()
	exempt.Store(id, bound)
	return func() { exempt.Delete(id) }
}

func goroutineID() uint64 {
	var buf [64]byte
	b := buf[:runtime.Stack(buf[:], false)]
	b = bytes.TrimPrefix(b, []byte("goroutine "))
	if i := bytes.IndexByte(b, ' '); i > 0 {
		b = b[:i]
	}
	n, _ := strconv.ParseUint(string(b), 10, 64)
	return n
}

// statementTimeoutSQL is the SET LOCAL to add after the search path, or "".
func statementTimeoutSQL(schema string) string {
	if StatementTimeoutFor == nil {
		return ""
	}
	d := StatementTimeoutFor(schema)
	if d <= 0 {
		return ""
	}
	if v, ok := exempt.Load(goroutineID()); ok {
		if bound := v.(time.Duration); bound > d {
			d = bound
		}
	}
	return "; SET LOCAL statement_timeout = " + strconv.FormatInt(d.Milliseconds(), 10)
}
