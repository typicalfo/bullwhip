//go:build !darwin && !linux

package bw

func readResources(string) (float64, float64, error) { return 0, 0, nil }
