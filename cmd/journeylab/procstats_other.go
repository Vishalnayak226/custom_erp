//go:build !windows

package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ReadProc reads a process's CPU time and resident memory from /proc.
func ReadProc(pid int) ProcSnap {
	if pid <= 0 {
		return ProcSnap{}
	}
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return ProcSnap{}
	}
	s := string(b)
	i := strings.LastIndex(s, ")")
	if i < 0 {
		return ProcSnap{}
	}
	f := strings.Fields(s[i+1:])
	if len(f) < 13 {
		return ProcSnap{}
	}
	ut, _ := strconv.ParseFloat(f[11], 64)
	st, _ := strconv.ParseFloat(f[12], 64)
	snap := ProcSnap{OK: true, CPUMS: (ut + st) * 10} // 100 ticks per second
	if sb, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid)); err == nil {
		for _, ln := range strings.Split(string(sb), "\n") {
			if strings.HasPrefix(ln, "VmRSS:") {
				snap.RSSKB, _ = strconv.ParseInt(strings.Fields(ln)[1], 10, 64)
			}
			if strings.HasPrefix(ln, "VmHWM:") {
				snap.PeakKB, _ = strconv.ParseInt(strings.Fields(ln)[1], 10, 64)
			}
		}
	}
	return snap
}
