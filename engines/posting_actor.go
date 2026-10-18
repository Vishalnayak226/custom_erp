package engines

import (
	"bytes"
	"database/sql"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Stage 73.1.9: every GL row names who posted it.
//
// PostDoubleEntryTx is the one place every GL row is written, but none of its
// ~30 callers is handed a user, and the plan forbids changing a posting
// signature (other work posts through it concurrently). So the actor travels
// beside the call instead of through it: the request middleware, the public
// API middleware and the job runner bind a PostingActor to the goroutine that
// serves the request or runs the job, and PostDoubleEntryTx reads it back.
//
// A posting made with nothing bound - a background ticker, a test, a goroutine
// a handler spawned - is still attributed, never left NULL: posted_by is
// "service:background" and source_command names the engine function that
// asked for the posting. NULL is reserved for rows written before this stage.

// PostingActor is who (PostedBy) did what (SourceCommand).
type PostingActor struct {
	PostedBy      string
	SourceCommand string
}

var postingActors sync.Map // goroutine id (uint64) -> PostingActor

// BindPostingActor attaches actor to the calling goroutine until the returned
// release runs. Bindings nest: release restores whatever was bound before, so
// a request that runs a job inline gets its own attribution back afterwards.
func BindPostingActor(actor PostingActor) (release func()) {
	id := currentGoroutineID()
	if id == 0 {
		return func() {}
	}
	actor.PostedBy = truncateAttribution(actor.PostedBy, 150)
	actor.SourceCommand = truncateAttribution(actor.SourceCommand, 200)
	prev, hadPrev := postingActors.Load(id)
	postingActors.Store(id, actor)
	return func() {
		if hadPrev {
			postingActors.Store(id, prev)
		} else {
			postingActors.Delete(id)
		}
	}
}

// RequestPostingActor names the actor of one authenticated API request.
// purpose is the token's purpose claim ("extension" tokens carry no user);
// command is "<METHOD> <route pattern or path>".
func RequestPostingActor(userID, username, purpose, command string) PostingActor {
	actor := PostingActor{SourceCommand: command}
	switch {
	case username != "":
		actor.PostedBy = username
	case userID != "":
		actor.PostedBy = "user:" + userID
	case purpose == "extension":
		actor.PostedBy = "service:extension"
	default:
		// A public route (webhook, callback) - no user signed in.
		actor.PostedBy = "service:inbound"
	}
	return actor
}

// currentPostingActor is what PostDoubleEntryTx stamps on the rows it writes.
func currentPostingActor() PostingActor {
	if id := currentGoroutineID(); id != 0 {
		if v, ok := postingActors.Load(id); ok {
			return v.(PostingActor)
		}
	}
	return PostingActor{PostedBy: "service:background", SourceCommand: postingCallerCommand()}
}

// postingCallerCommand names the first engine function outside the posting
// functions themselves - the business operation that asked for the posting.
func postingCallerCommand() string {
	pcs := make([]uintptr, 16)
	n := runtime.Callers(3, pcs)
	frames := runtime.CallersFrames(pcs[:n])
	for {
		frame, more := frames.Next()
		name := frame.Function
		if i := strings.LastIndex(name, "/"); i >= 0 {
			name = name[i+1:]
		}
		if name != "" && !strings.Contains(name, "PostDoubleEntry") && !strings.Contains(name, "PostingActor") {
			return truncateAttribution("func:"+name, 200)
		}
		if !more {
			return "func:unknown"
		}
	}
}

// currentGoroutineID parses "goroutine 123 [running]:" from the runtime's own
// stack header. Ids are never reused, but one goroutine can serve many units
// of work (net/http keep-alive, the job loop), so every binder releases with
// defer - a panic must not leave one request's user on the next.
func currentGoroutineID() uint64 {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	b := bytes.TrimPrefix(buf[:n], []byte("goroutine "))
	i := bytes.IndexByte(b, ' ')
	if i <= 0 {
		return 0
	}
	id, err := strconv.ParseUint(string(b[:i]), 10, 64)
	if err != nil {
		return 0
	}
	return id
}

func truncateAttribution(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}

// glAttributionReady reports whether this schema's gl_postings has the
// 73.1.9 columns. A binary deployed ahead of its migration (or a test run
// against a database other sessions migrate) must keep posting rather than
// fail every sale, so the INSERT only names the columns once they exist. A
// positive answer is cached for the process; a negative one is rechecked
// after a minute, so applying the migration takes effect without a restart.
var glAttributionColumns sync.Map // schema -> glAttributionCheck

type glAttributionCheck struct {
	ready     bool
	checkedAt time.Time
}

func glAttributionReady(tx *sql.Tx, schema string) bool {
	if v, ok := glAttributionColumns.Load(schema); ok {
		c := v.(glAttributionCheck)
		if c.ready || time.Since(c.checkedAt) < time.Minute {
			return c.ready
		}
	}
	var ready bool
	if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = 'gl_postings' AND column_name = 'source_command')`, schema).Scan(&ready); err != nil {
		return false
	}
	glAttributionColumns.Store(schema, glAttributionCheck{ready: ready, checkedAt: time.Now()})
	return ready
}
