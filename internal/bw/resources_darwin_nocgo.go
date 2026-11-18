//go:build darwin && !cgo

package bw

import "fmt"

func availableMemory() (float64, error) {
	return 0, fmt.Errorf("macOS memory readings require a bw build with cgo enabled")
}
