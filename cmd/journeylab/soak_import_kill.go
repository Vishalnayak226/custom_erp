package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// soak_import_kill.go - 313.3's one real crash. The engines' crash matrix
// (engines/import_crash_test.go) stops an import at every point inside one
// process; this kills the server process itself. A file goes up as a
// resumable upload, a watcher reads the batch's step, and the server is
// killed hard while it checks the rows. The restarted server must carry the
// import on by itself once the dead one's claim runs out (up to the job
// lease, 5 minutes); the import then commits and must end as a run with no
// crash ends: every valid row written once with its quantity, one ImportJob,
// a reconcile that balances.

const (
	importKillDoctype = "POSCostingGap"
	importKillRows    = 60000
	importKillPiece   = 512 << 10
)

// importKillCSV is n rows; every 97th has a qty that is not a number and the
// last repeats the first row's id, as the engines' bulk tests have it. It
// returns the file, how many rows are valid and their total quantity.
func importKillCSV(prefix string, n int) (body []byte, valid, qty int) {
	var b strings.Builder
	b.WriteString("id,cart_number,sku,qty,sale_value,status\n")
	for i := 0; i < n; i++ {
		q := strconv.Itoa(i%7 + 1)
		if i%97 == 50 {
			q = "abc"
		} else {
			valid++
			qty += i%7 + 1
		}
		fmt.Fprintf(&b, "%s-%06d,C%d,SKU%d,%s,%d.25,Open\n", prefix, i, i%13, i, q, i%500)
	}
	fmt.Fprintf(&b, "%s-%06d,C0,SKU0,1,1,Open\n", prefix, 0)
	return []byte(b.String()), valid, qty
}

// importKill sends the file, kills the server through restart (which kills
// it hard, starts it again and waits until it serves) once the rows are being
// checked, then takes the import to the end and checks what it wrote.
func importKill(r *Runner, restart func() (time.Duration, bool)) Disturbance {
	d := Disturbance{Name: "import killed while it checks its rows"}
	end := func(status, f string, a ...interface{}) Disturbance {
		d.Status, d.Detail = status, fmt.Sprintf(f, a...)
		return d
	}
	call := func(method, path string, body interface{}, headers map[string]string) (*Resp, error) {
		resp, err := r.Cl.Do("admin", method, path, body, CallOpts{Setup: true, NoRecord: true, Headers: headers})
		if err == nil && resp.Status >= 300 {
			err = fmt.Errorf("%s %s: %d %s", method, pathOnly(path), resp.Status, trunc(string(resp.Body), 200))
		}
		return resp, err
	}
	prefix := fmt.Sprintf("JLKILL%d", time.Now().Unix()%1000000)
	body, valid, qty := importKillCSV(prefix, importKillRows)

	up, err := call("POST", "/api/v1/uploads", map[string]interface{}{"file_name": prefix + ".csv", "size": len(body), "purpose": "import", "doctype": importKillDoctype}, nil)
	if err != nil {
		return end("fail", "start the upload: %v", err)
	}
	upID := jstr(up.JSON, "id")
	for at := 0; at < len(body); at += importKillPiece {
		to := min(at+importKillPiece, len(body))
		if _, err := call("PUT", "/api/v1/uploads/"+upID, string(body[at:to]), map[string]string{"Content-Type": "application/octet-stream", "Upload-Offset": strconv.Itoa(at)}); err != nil {
			return end("fail", "send the file: %v", err)
		}
	}
	staged, err := call("POST", "/api/v1/import-batches", map[string]interface{}{"upload_id": upID, "doctype": importKillDoctype}, nil)
	if err != nil {
		return end("fail", "stage the file: %v", err)
	}
	batchID := jstr(staged.JSON, "id")

	// batch reads the batch; work is nil once its step has ended.
	batch := func() (v, work map[string]interface{}, err error) {
		resp, err := call("GET", "/api/v1/import-batches/"+batchID, nil, nil)
		if err != nil {
			return nil, nil, err
		}
		work, _ = resp.JSON["work"].(map[string]interface{})
		if e := jstr(work, "error"); e != "" {
			return nil, nil, fmt.Errorf("the import stopped: %s", e)
		}
		return resp.JSON, work, nil
	}
	// until waits for the batch's step to end, up to max.
	until := func(max time.Duration) (map[string]interface{}, error) {
		for deadline := time.Now().Add(max); time.Now().Before(deadline); time.Sleep(time.Second) {
			v, work, err := batch()
			if err != nil || work == nil {
				return v, err
			}
		}
		return nil, fmt.Errorf("still working after %s", max)
	}

	// The watcher: kill the server once some rows are checked, not all.
	var checked float64
	for deadline := time.Now().Add(3 * time.Minute); ; time.Sleep(100 * time.Millisecond) {
		v, work, err := batch()
		if err != nil {
			return end("fail", "watch the import: %v", err)
		}
		if work == nil {
			return end("not_exercised", "the rows were read and checked (%s) before the watcher saw the check under way; send a bigger file", jstr(v, "stage"))
		}
		if jstr(work, "step") == "validate" && jnum(work, "done") > 0 {
			checked = jnum(work, "done")
			break
		}
		if time.Now().After(deadline) {
			return end("fail", "the check had not started after 3 minutes (%s, %v rows done)", jstr(work, "step"), jnum(work, "done"))
		}
	}
	killed := time.Now()
	healthyAfter, ok := restart()
	if !ok {
		return end("fail", "the server did not come back after the kill")
	}
	v, err := until(8 * time.Minute)
	if err != nil {
		return end("fail", "after the kill: %v", err)
	}
	if jstr(v, "stage") != "validated" {
		return end("fail", "after the kill the batch is %s, not validated", jstr(v, "stage"))
	}
	resumed := time.Since(killed)

	for _, step := range []string{"preview", "approve", "commit"} {
		if _, err := call("POST", "/api/v1/import-batches/"+batchID+"/"+step, nil, nil); err != nil {
			return end("fail", "%s: %v", step, err)
		}
	}
	committing := time.Now()
	if v, err = until(10 * time.Minute); err != nil {
		return end("fail", "commit: %v", err)
	}

	var docs, qtySum, jobs int
	if err := r.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*), COALESCE(SUM((data->>'qty')::numeric), 0)::bigint FROM %s.documents
		WHERE doctype = $1 AND id LIKE $2 AND deleted_at IS NULL`, r.Cfg.Schema), importKillDoctype, prefix+"-%").Scan(&docs, &qtySum); err != nil {
		return end("fail", "count the records: %v", err)
	}
	if err := r.DB.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s.documents WHERE doctype = 'ImportJob' AND data->>'batch_id' = $1`, r.Cfg.Schema), batchID).Scan(&jobs); err != nil {
		return end("fail", "count the import jobs: %v", err)
	}
	d.Measures = []string{
		fmt.Sprintf("%d rows sent; killed with %.0f checked", importKillRows+1, checked),
		fmt.Sprintf("healthy again after %.1f s; the check carried on by itself and ended %.0f s after the kill", healthyAfter.Seconds(), resumed.Seconds()),
		fmt.Sprintf("committed and reconciled %.0f s after approval", time.Since(committing).Seconds()),
		fmt.Sprintf("%d records, quantity %d, %d ImportJob", docs, qtySum, jobs),
	}
	committed := int(jnum(v, "counts", "committed"))
	if jstr(v, "stage") != "reconciled" || jstr(v, "reconcile", "balanced") != "true" || committed != valid || jnum(v, "counts", "failed") != 0 ||
		docs != valid || qtySum != qty || jobs != 1 {
		return end("fail", "the import ended %s, %d committed, reconcile balanced=%s; want %d valid rows written once, quantity %d, one ImportJob",
			jstr(v, "stage"), committed, jstr(v, "reconcile", "balanced"), valid, qty)
	}
	return end("pass", "server killed hard while the import checked its rows; the restarted server carried it on by itself and it ended as a run with no crash")
}
