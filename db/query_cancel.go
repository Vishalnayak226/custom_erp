package db

import (
	"context"
	"database/sql/driver"
	"sync"
	"sync/atomic"

	"github.com/lib/pq"
)

// 283.3: a query a user walked away from stops in Postgres.
//
// About 8,600 database calls in this codebase take no context (db.DB.Query,
// tx.Exec ...), so when a user closes a report the Go side stops waiting but
// Postgres keeps running the query until platform.db_statement_timeout_seconds
// (if set) or the end. Rather than thread a context through every report's
// Run, a caller binds one to its goroutine for as long as it runs the work:
//
//	defer db.BindContext(ctx)()
//
// Every query, exec, transaction and prepare made on that goroutine without a
// context of its own then runs under ctx, and lib/pq cancels the statement in
// Postgres the moment ctx ends (the connection is then dropped from the pool,
// as lib/pq always does after a cancel). A call that already passes its own
// context keeps it. Goroutines started by the work are not bound. With no
// binding in force anywhere the only cost per call is one atomic read.
//
// The goroutine id comes from the stack header, as ExemptStatementTimeout's
// does (statement_timeout.go).

var (
	boundCtx   sync.Map // goroutine id -> context.Context
	boundCount atomic.Int64
)

// BindContext runs the calling goroutine's context-free database calls under
// ctx until the returned release is called. Bindings nest: the release puts
// back the one before.
func BindContext(ctx context.Context) (release func()) {
	if ctx == nil || ctx.Done() == nil {
		return func() {}
	}
	id := goroutineID()
	prev, hadPrev := boundCtx.Load(id)
	boundCtx.Store(id, ctx)
	boundCount.Add(1)
	return func() {
		if hadPrev {
			boundCtx.Store(id, prev)
		} else {
			boundCtx.Delete(id)
		}
		boundCount.Add(-1)
	}
}

// boundContext is ctx, or the calling goroutine's bound context when ctx has
// no cancellation of its own (context.Background, as database/sql passes for
// a call made without one).
func boundContext(ctx context.Context) context.Context {
	if boundCount.Load() == 0 || ctx.Done() != nil {
		return ctx
	}
	if v, ok := boundCtx.Load(goroutineID()); ok {
		return v.(context.Context)
	}
	return ctx
}

// cancelConnector opens lib/pq connections exactly as sql.Open("postgres",
// dsn) does and wraps each one so a bound context reaches it.
type cancelConnector struct{ dsn string }

func (c cancelConnector) Connect(context.Context) (driver.Conn, error) {
	cn, err := pq.Open(c.dsn)
	if err != nil {
		return nil, err
	}
	return wrapCancelConn(cn), nil
}

func (cancelConnector) Driver() driver.Driver { return pq.Driver{} }

// pqConn is every interface lib/pq's connection gives database/sql; the
// wrapper forwards them all so nothing database/sql does changes.
type pqConn interface {
	driver.Conn
	driver.ConnBeginTx
	driver.ConnPrepareContext
	driver.QueryerContext
	driver.ExecerContext
	driver.Pinger
	driver.SessionResetter
	driver.Validator
	driver.NamedValueChecker
}

type cancelConn struct{ pqConn }

// wrapCancelConn wraps a lib/pq connection. Should a lib/pq upgrade stop
// offering one of the interfaces above, the connection is used as it is
// (TestBindContextWrapsEveryConnection fails first).
func wrapCancelConn(cn driver.Conn) driver.Conn {
	if pc, ok := cn.(pqConn); ok {
		return cancelConn{pc}
	}
	return cn
}

func (c cancelConn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	return c.pqConn.QueryContext(boundContext(ctx), query, args)
}

func (c cancelConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	return c.pqConn.ExecContext(boundContext(ctx), query, args)
}

func (c cancelConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	return c.pqConn.BeginTx(boundContext(ctx), opts)
}

func (c cancelConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	return c.pqConn.PrepareContext(boundContext(ctx), query)
}
