package main

import (
	"bytes"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// pgTarget is a database URL taken apart for the command-line tools.
type pgTarget struct {
	Host, Port, User, Pass, DB string
}

func parsePG(raw string) (pgTarget, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return pgTarget{}, err
	}
	t := pgTarget{Host: u.Hostname(), Port: u.Port(), DB: strings.TrimPrefix(u.Path, "/")}
	if t.Port == "" {
		t.Port = "5432"
	}
	if u.User != nil {
		t.User = u.User.Username()
		t.Pass, _ = u.User.Password()
	}
	if t.User == "" {
		t.User = "postgres"
	}
	return t, nil
}

func (t pgTarget) URL(db string) string {
	cred := url.User(t.User)
	if t.Pass != "" {
		cred = url.UserPassword(t.User, t.Pass)
	}
	return (&url.URL{Scheme: "postgres", User: cred, Host: t.Host + ":" + t.Port, Path: "/" + db, RawQuery: "sslmode=disable"}).String()
}

func (t pgTarget) env() []string {
	e := os.Environ()
	if t.Pass != "" {
		e = append(e, "PGPASSWORD="+t.Pass)
	}
	return e
}

func (r *Runner) pgTool(name string) string {
	exe := name
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	if r.Cfg.PGBin != "" {
		return filepath.Join(r.Cfg.PGBin, exe)
	}
	return exe
}

func runCmd(env []string, bin string, args ...string) (string, error) {
	cmd := exec.Command(bin, args...)
	cmd.Env = env
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	return out.String(), err
}

// JourneyIncident is J7: take a backup, restore it somewhere else, prove the
// copy is whole with the product's own restore check, and measure how long it
// took. It never touches the live database except to read it.
func (r *Runner) JourneyIncident() JourneyRecord {
	j := r.NewJourney("J7", "Incident recovery (backup and restore drill)")
	tgt, err := parsePG(r.Cfg.DBURL)
	if err != nil || r.Cfg.ServerBin == "" {
		j.Rec.Status = "not_driven"
		j.Rec.Reason = "needs -db-url and -server-bin (the server binary runs -verify-restore on the restored copy)"
		return j.Rec
	}
	scratch := tgt.DB + "_restore_scratch"
	dump := filepath.Join(os.TempDir(), fmt.Sprintf("journeylab-%d.dump", time.Now().UnixNano()))
	defer os.Remove(dump)
	var dumpBytes int64
	var t0 time.Time
	var liveRows, liveAfter, restoredRows map[string]int64

	rowCounts := func(url string) (map[string]int64, error) {
		m := map[string]int64{}
		for _, tbl := range []string{"documents", "gl_postings", "audit_logs", "inventory_availability"} {
			var n int64
			args := []string{"-At", "-c", fmt.Sprintf("SELECT COUNT(*) FROM %s.%s", r.Cfg.Schema, tbl), url}
			out, err := runCmd(tgt.env(), r.pgTool("psql"), args...)
			if err != nil {
				return nil, fmt.Errorf("count %s: %v %s", tbl, err, trunc(out, 120))
			}
			fmt.Sscan(strings.TrimSpace(out), &n)
			m[tbl] = n
		}
		return m, nil
	}

	j.Step("take a backup (pg_dump)", "operator", func(s *StepCtx) error {
		t0 = time.Now()
		var err error
		liveRows, err = rowCounts(tgt.URL(tgt.DB))
		if err != nil {
			return err
		}
		out, err := runCmd(tgt.env(), r.pgTool("pg_dump"), "-Fc", "-f", dump, "-h", tgt.Host, "-p", tgt.Port, "-U", tgt.User, tgt.DB)
		if err != nil {
			return fmt.Errorf("pg_dump: %v %s", err, trunc(out, 200))
		}
		// the server keeps writing in the background (audit, jobs), so the live
		// counts are read on both sides of the dump and the copy must fall between them
		if liveAfter, err = rowCounts(tgt.URL(tgt.DB)); err != nil {
			return err
		}
		st, err := os.Stat(dump)
		if err != nil {
			return err
		}
		dumpBytes = st.Size()
		s.Check(dumpBytes > 0, "backup file is not empty")
		j.Rec.Evidence = append(j.Rec.Evidence, fmt.Sprintf("backup: %d bytes", dumpBytes))
		return nil
	})
	j.Step("restore into a scratch database", "operator", func(s *StepCtx) error {
		_, _ = runCmd(tgt.env(), r.pgTool("dropdb"), "--if-exists", "-h", tgt.Host, "-p", tgt.Port, "-U", tgt.User, scratch)
		if out, err := runCmd(tgt.env(), r.pgTool("createdb"), "-h", tgt.Host, "-p", tgt.Port, "-U", tgt.User, scratch); err != nil {
			return fmt.Errorf("createdb: %v %s", err, trunc(out, 200))
		}
		out, err := runCmd(tgt.env(), r.pgTool("pg_restore"), "--no-owner", "-h", tgt.Host, "-p", tgt.Port, "-U", tgt.User, "-d", scratch, dump)
		// pg_restore reports harmless warnings (extension comments) with a non-zero code; judge by the data, below.
		if err != nil {
			j.Rec.Evidence = append(j.Rec.Evidence, "pg_restore returned warnings: "+trunc(strings.TrimSpace(out), 160))
		}
		// counted now: the product's restore check, which runs next, writes its own rows into the copy
		var cerr error
		restoredRows, cerr = rowCounts(tgt.URL(scratch))
		return cerr
	})
	j.Step("the restored copy passes the product's restore check", "operator", func(s *StepCtx) error {
		env := append(tgt.env(), "DATABASE_URL="+tgt.URL(scratch))
		out, err := runCmd(env, r.Cfg.ServerBin, "-verify-restore")
		s.Check(err == nil, "server -verify-restore failed: %v %s", err, trunc(out, 300))
		s.Check(!strings.Contains(out, "FAIL"), "no blocking FAIL line in the restore check")
		return nil
	})
	j.Step("restored row counts equal the live ones", "operator", func(s *StepCtx) error {
		got := restoredRows
		for k, v := range liveRows {
			s.Check(got[k] >= v && got[k] <= liveAfter[k], "%s: live %d before the dump and %d after it, restored %d", k, v, liveAfter[k], got[k])
		}
		j.Rec.Evidence = append(j.Rec.Evidence, fmt.Sprintf("backup to verified restore: %.1f s for %d bytes", time.Since(t0).Seconds(), dumpBytes))
		return nil
	})
	j.Step("drop the scratch copy", "operator", func(s *StepCtx) error {
		out, err := runCmd(tgt.env(), r.pgTool("dropdb"), "-h", tgt.Host, "-p", tgt.Port, "-U", tgt.User, scratch)
		if err != nil {
			return fmt.Errorf("dropdb: %v %s", err, trunc(out, 160))
		}
		return nil
	})
	return j.Finish(j.Reconcile())
}
