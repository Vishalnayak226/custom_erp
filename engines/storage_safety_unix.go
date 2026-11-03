//go:build !windows

package engines

import "syscall"

// diskFreeAndTotal is the free and total bytes of the disk the server runs on.
func diskFreeAndTotal() (uint64, uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(".", &st); err != nil {
		return 0, 0, err
	}
	return st.Bavail * uint64(st.Bsize), st.Blocks * uint64(st.Bsize), nil
}
