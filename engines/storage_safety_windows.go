//go:build windows

package engines

import (
	"syscall"
	"unsafe"
)

// diskFreeAndTotal is the free and total bytes of the disk the server runs on.
func diskFreeAndTotal() (uint64, uint64, error) {
	p, err := syscall.UTF16PtrFromString(".")
	if err != nil {
		return 0, 0, err
	}
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("GetDiskFreeSpaceExW")
	var avail, total, free uint64
	r, _, e := proc.Call(uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&avail)), uintptr(unsafe.Pointer(&total)), uintptr(unsafe.Pointer(&free)))
	if r == 0 {
		return 0, 0, e
	}
	return avail, total, nil
}
