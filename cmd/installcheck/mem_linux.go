//go:build linux

package main

import (
	"bufio"
	"errors"
	"os"
	"strconv"
	"strings"
)

func memTotal() (uint64, error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if rest, ok := strings.CutPrefix(sc.Text(), "MemTotal:"); ok {
			fields := strings.Fields(rest)
			if len(fields) == 0 {
				break
			}
			kb, err := strconv.ParseUint(fields[0], 10, 64)
			return kb << 10, err
		}
	}
	return 0, errors.New("MemTotal not found")
}
