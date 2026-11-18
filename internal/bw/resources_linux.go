package bw

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

func availableMemory() (float64, error) {
	data, e := os.ReadFile("/proc/meminfo")
	if e != nil {
		return 0, e
	}
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && f[0] == "MemAvailable:" {
			n, e := strconv.ParseFloat(f[1], 64)
			return n * 1024, e
		}
	}
	return 0, fmt.Errorf("MemAvailable is absent from /proc/meminfo")
}
