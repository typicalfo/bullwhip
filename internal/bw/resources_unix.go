//go:build darwin || linux

package bw

import "golang.org/x/sys/unix"

func readResources(root string) (float64, float64, error) {
	if root == "" {
		root = "."
	}
	var stat unix.Statfs_t
	if e := unix.Statfs(root, &stat); e != nil {
		return 0, 0, e
	}
	mem, e := availableMemory()
	return float64(stat.Bavail) * float64(stat.Bsize) / 1e9, mem / 1e9, e
}
