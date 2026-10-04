package server

import (
	"context"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"time"

	"custom_erp/db"
)

// BLD-052: health and readiness are different questions, and this codebase
// only answered the first one.
//
// /api/v1/health is liveness: "is this process alive and is its database
// reachable" - what a process supervisor restarts on. Restarting is the right
// response to a dead process and the wrong response to a process that is
// alive but not yet fit to serve, so answering both from one endpoint forces
// a load balancer and a supervisor to share a single verdict.
//
// /api/v1/ready is readiness: "should this process receive traffic right
// now". It is false while the schema is behind the binary's own migrations
// (the drift class Stage 30.2.2 exists to catch: a deployed binary expecting
// columns the database does not have serves 500s on the affected endpoints
// while looking perfectly healthy), and false once shutdown has begun, so a
// draining instance stops being sent new requests while it finishes the ones
// it has.
//
// Deliberately no new dependency and no probe framework: two checks, a JSON
// body, and one atomic flag.

// shuttingDown is set once the graceful-shutdown path begins. Readiness flips
// to false immediately, before srv.Shutdown stops accepting connections, so a
// load balancer polling readiness drains this instance instead of discovering
// it mid-request.
var shuttingDown atomic.Bool

// markShuttingDown is called from the signal handler in routes.go.
func markShuttingDown() { shuttingDown.Store(true) }

// readinessDBTimeout bounds the readiness probe's own database work. A probe
// that hangs is a probe that tells the load balancer nothing, which is worse
// than one that answers "not ready" - so an unresponsive database fails the
// check rather than blocking the poller.
const readinessDBTimeout = 3 * time.Second

func handleReadiness(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIErrorGeneric(w, r, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}

	checks := map[string]interface{}{}
	ready := true

	if shuttingDown.Load() {
		checks["accepting_traffic"] = map[string]interface{}{"ok": false, "detail": "process is draining for shutdown"}
		ready = false
	} else {
		checks["accepting_traffic"] = map[string]interface{}{"ok": true}
	}

	ctx, cancel := context.WithTimeout(r.Context(), readinessDBTimeout)
	defer cancel()
	if err := db.DB.PingContext(ctx); err != nil {
		// The driver error can name a host, port or user, so it is recorded
		// server-side and summarised here - readiness is reachable without a
		// bearer token (same tier as /health and /version), so its body is
		// effectively public.
		checks["database"] = map[string]interface{}{"ok": false, "detail": "database is not reachable"}
		ready = false
	} else {
		checks["database"] = map[string]interface{}{"ok": true}
	}

	// Schema drift. Reported by count and by name: the names are migration
	// filenames from this repository, not tenant data, and knowing *which*
	// migration is missing is the whole value of the check during a deploy.
	if pending, err := db.PendingMigrations(); err != nil {
		checks["migrations"] = map[string]interface{}{"ok": false, "detail": "could not determine migration state"}
		ready = false
	} else if len(pending) > 0 {
		checks["migrations"] = map[string]interface{}{"ok": false, "pending": len(pending), "pending_migrations": pending}
		ready = false
	} else {
		checks["migrations"] = map[string]interface{}{"ok": true, "pending": 0}
	}

	status := "ready"
	code := http.StatusOK
	if !ready {
		status = "not_ready"
		// 503 so a load balancer and deploy script treat it as "do not send
		// traffic" without needing to parse the body.
		code = http.StatusServiceUnavailable
	}
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"status":     status,
		"git_commit": gitCommit,
		"build_time": buildTime,
		"checks":     checks,
	})
}
