//go:build !linux && !windows

package main

import "errors"

func memTotal() (uint64, error) { return 0, errors.New("unsupported") }
