//go:build windows

package main

import (
	"syscall"
	"unsafe"
)

var (
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	procOpenProcess  = kernel32.NewProc("OpenProcess")
	procGetTimes     = kernel32.NewProc("GetProcessTimes")
	procGetMemInfo   = kernel32.NewProc("K32GetProcessMemoryInfo")
	procCloseHandle  = kernel32.NewProc("CloseHandle")
	procQueryLimited = uintptr(0x1000) // PROCESS_QUERY_LIMITED_INFORMATION
)

type processMemoryCounters struct {
	Cb                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
}

// ReadProc reads a process's CPU time and working set through kernel32, with no
// dependency beyond the standard library.
func ReadProc(pid int) ProcSnap {
	if pid <= 0 {
		return ProcSnap{}
	}
	h, _, _ := procOpenProcess.Call(procQueryLimited, 0, uintptr(pid))
	if h == 0 {
		return ProcSnap{}
	}
	defer procCloseHandle.Call(h)
	var c, e, k, u syscall.Filetime
	if r, _, _ := procGetTimes.Call(h, uintptr(unsafe.Pointer(&c)), uintptr(unsafe.Pointer(&e)), uintptr(unsafe.Pointer(&k)), uintptr(unsafe.Pointer(&u))); r == 0 {
		return ProcSnap{}
	}
	ft := func(f syscall.Filetime) float64 {
		return float64(uint64(f.HighDateTime)<<32|uint64(f.LowDateTime)) / 10000 // 100ns ticks to ms
	}
	var m processMemoryCounters
	m.Cb = uint32(unsafe.Sizeof(m))
	if r, _, _ := procGetMemInfo.Call(h, uintptr(unsafe.Pointer(&m)), uintptr(m.Cb)); r == 0 {
		return ProcSnap{}
	}
	return ProcSnap{OK: true, CPUMS: ft(k) + ft(u), RSSKB: int64(m.WorkingSetSize) / 1024, PeakKB: int64(m.PeakWorkingSetSize) / 1024}
}
