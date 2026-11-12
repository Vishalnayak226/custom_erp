package main

import (
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// A private, throwaway Postgres cluster for the soak. The soak restarts the
// database on purpose, so it must never do that to a shared instance: this file
// only ever manages a cluster it created itself (marked by a file in its folder)
// on a port that was free, and refuses the usual shared ports.

const clusterMarker = "journeylab.cluster"

var sharedPorts = map[int]bool{5432: true, 5433: true, 5490: true}

// FreePort finds a free local TCP port at or above start.
func FreePort(start int) (int, error) {
	for p := start; p < start+200; p++ {
		l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
		if err == nil {
			l.Close()
			return p, nil
		}
	}
	return 0, fmt.Errorf("no free port from %d", start)
}

// Cluster is one private Postgres.
type Cluster struct {
	BinDir string
	Dir    string
	Port   int
}

func (c Cluster) tool(n string) string {
	if runtime.GOOS == "windows" {
		n += ".exe"
	}
	if c.BinDir == "" {
		return n
	}
	return filepath.Join(c.BinDir, n)
}

func (c Cluster) URL(db string) string {
	return fmt.Sprintf("postgres://postgres@127.0.0.1:%d/%s?sslmode=disable", c.Port, db)
}

func (c Cluster) own() error {
	if _, err := os.Stat(filepath.Join(c.Dir, clusterMarker)); err != nil {
		return fmt.Errorf("%s is not a journeylab cluster (no %s): refusing to manage it", c.Dir, clusterMarker)
	}
	if sharedPorts[c.Port] {
		return fmt.Errorf("port %d is a shared Postgres port: refusing", c.Port)
	}
	return nil
}

// Up creates the cluster (pg_stat_statements preloaded) and starts it.
func (c Cluster) Up() error {
	if sharedPorts[c.Port] {
		return fmt.Errorf("port %d is a shared Postgres port: refusing", c.Port)
	}
	if _, err := os.Stat(filepath.Join(c.Dir, "PG_VERSION")); err == nil {
		return fmt.Errorf("%s already holds a cluster", c.Dir)
	}
	if out, err := runCmd(os.Environ(), c.tool("initdb"), "-D", c.Dir, "-U", "postgres", "-A", "trust", "-E", "UTF8", "--locale=C"); err != nil {
		return fmt.Errorf("initdb: %v %s", err, trunc(out, 300))
	}
	if err := os.WriteFile(filepath.Join(c.Dir, clusterMarker), []byte("created by journeylab\n"), 0o644); err != nil {
		return err
	}
	conf := fmt.Sprintf("\nport = %d\nlisten_addresses = '127.0.0.1'\nshared_preload_libraries = 'pg_stat_statements'\nfsync = on\nmax_connections = 100\ntimezone = 'Asia/Calcutta'\n", c.Port)
	f, err := os.OpenFile(filepath.Join(c.Dir, "postgresql.conf"), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, err = f.WriteString(conf)
	f.Close()
	if err != nil {
		return err
	}
	return c.Start()
}

func (c Cluster) ctl(args ...string) error {
	if err := c.own(); err != nil {
		return err
	}
	if args[0] == "start" || args[0] == "restart" {
		// pg_ctl leaves the server running with our output pipe inherited, so
		// waiting on a captured pipe would never end; send it to the null device
		// (the server's own log file keeps the detail).
		cmd := exec.Command(c.tool("pg_ctl"), append([]string{"-D", c.Dir}, args...)...)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("pg_ctl %s: %v (see %s)", strings.Join(args, " "), err, filepath.Join(c.Dir, "server.log"))
		}
		return nil
	}
	out, err := runCmd(os.Environ(), c.tool("pg_ctl"), append([]string{"-D", c.Dir}, args...)...)
	if err != nil {
		return fmt.Errorf("pg_ctl %s: %v %s", strings.Join(args, " "), err, trunc(out, 300))
	}
	return nil
}

func (c Cluster) Start() error {
	return c.ctl("start", "-w", "-t", "60", "-l", filepath.Join(c.Dir, "server.log"))
}
func (c Cluster) Stop() error    { return c.ctl("stop", "-m", "fast", "-w", "-t", "60") }
func (c Cluster) Restart() error { return c.ctl("restart", "-m", "fast", "-w", "-t", "60") }

// CreateDB makes a database and installs pg_stat_statements in it.
func (c Cluster) CreateDB(name string) error {
	// pg_ctl start -w can return a moment before the port accepts connections on Windows.
	var out string
	var err error
	for i := 0; i < 30; i++ {
		out, err = runCmd(os.Environ(), c.tool("createdb"), "-h", "127.0.0.1", "-p", fmt.Sprint(c.Port), "-U", "postgres", name)
		if err == nil || !strings.Contains(out, "Connection refused") {
			break
		}
		time.Sleep(time.Second)
	}
	if err != nil {
		return fmt.Errorf("createdb: %v %s", err, trunc(out, 200))
	}
	if out, err := runCmd(os.Environ(), c.tool("psql"), "-c", "CREATE EXTENSION IF NOT EXISTS pg_stat_statements", c.URL(name)); err != nil {
		return fmt.Errorf("create extension: %v %s", err, trunc(out, 200))
	}
	return nil
}

func cmdCluster(args []string) int {
	if len(args) < 1 || (args[0] != "up" && args[0] != "down") {
		fmt.Fprintln(os.Stderr, "usage: journeylab cluster up|down -pg-bin <dir> -dir <dir> [-port N] [-db name]")
		return 2
	}
	fs := flag.NewFlagSet("cluster", flag.ExitOnError)
	bin := fs.String("pg-bin", "", "folder holding initdb, pg_ctl, createdb, psql")
	dir := fs.String("dir", filepath.Join(os.TempDir(), "journeylab-cluster"), "the cluster's data folder")
	port := fs.Int("port", 0, "port (0 = first free from 5600)")
	dbn := fs.String("db", "", "with up: also create this database (with pg_stat_statements)")
	_ = fs.Parse(args[1:])
	c := Cluster{BinDir: *bin, Dir: *dir, Port: *port}
	if args[0] == "up" {
		if c.Port == 0 {
			p, err := FreePort(5600)
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				return 2
			}
			c.Port = p
		}
		if err := c.Up(); err != nil {
			fmt.Fprintln(os.Stderr, "journeylab:", err)
			return 1
		}
		if *dbn != "" {
			time.Sleep(500 * time.Millisecond)
			if err := c.CreateDB(*dbn); err != nil {
				fmt.Fprintln(os.Stderr, "journeylab:", err)
				return 1
			}
		}
		fmt.Printf("cluster up on port %d in %s\n", c.Port, c.Dir)
		return 0
	}
	if err := c.Stop(); err != nil {
		fmt.Fprintln(os.Stderr, "journeylab:", err)
		return 1
	}
	fmt.Println("cluster stopped")
	return 0
}
