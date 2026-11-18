package bw

import (
	"fmt"
	"math"
	"os"
	"runtime"
	"strconv"
	"strings"
)

type ResourceLimits struct {
	MinDisk        *float64 `json:"min_free_disk_gb,omitempty"`
	MinMemory      *float64 `json:"min_free_memory_gb,omitempty"`
	CriticalDisk   *float64 `json:"critical_free_disk_gb,omitempty"`
	CriticalMemory *float64 `json:"critical_free_memory_gb,omitempty"`
	MaxClaims      *int     `json:"max_claims,omitempty"`
}

func (r ResourceLimits) validate() error {
	for _, v := range []*float64{r.MinDisk, r.MinMemory, r.CriticalDisk, r.CriticalMemory} {
		if v != nil && (math.IsNaN(*v) || math.IsInf(*v, 0) || *v < 0) {
			return fmt.Errorf("resource thresholds must be finite nonnegative numbers")
		}
	}
	if r.MaxClaims != nil && *r.MaxClaims < 1 {
		return fmt.Errorf("max_claims must be positive")
	}
	d, m, cd, cm := 10.0, 2.0, 3.0, 0.5
	if r.MinDisk != nil {
		d = *r.MinDisk
	}
	if r.MinMemory != nil {
		m = *r.MinMemory
	}
	if r.CriticalDisk != nil {
		cd = *r.CriticalDisk
	}
	if r.CriticalMemory != nil {
		cm = *r.CriticalMemory
	}
	if cd > d || cm > m {
		return fmt.Errorf("critical resource levels must not exceed minimums")
	}
	return nil
}

type ResourceLevel struct {
	Value    float64 `json:"value"`
	Source   string  `json:"source"`
	Variable string  `json:"variable"`
}
type ResourceReport struct {
	Enabled  bool                     `json:"enabled"`
	DiskGB   float64                  `json:"free_disk_gb"`
	MemoryGB float64                  `json:"free_memory_gb"`
	Claims   int                      `json:"claims"`
	Levels   map[string]ResourceLevel `json:"levels"`
	Status   string                   `json:"status"`
	Reason   string                   `json:"reason,omitempty"`
}

func resourceReport(c Catalog, state State, root string) (ResourceReport, error) {
	r := ResourceReport{Enabled: runtime.GOOS == "darwin" || runtime.GOOS == "linux", Levels: map[string]ResourceLevel{}, Status: "ok"}
	for _, m := range state.Tasks {
		if !m.Done {
			r.Claims++
		}
	}
	set := func(key, env string, def float64, p *float64) error {
		v := ResourceLevel{def, "default", env}
		if p != nil {
			v.Value, v.Source = *p, "catalog"
		}
		if raw, ok := os.LookupEnv(env); ok {
			n, e := strconv.ParseFloat(raw, 64)
			if e != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 {
				return fail(1, "%s must be a finite nonnegative number", env)
			}
			v.Value, v.Source = n, "environment"
		}
		r.Levels[key] = v
		return nil
	}
	for _, v := range []struct {
		k, e string
		d    float64
		p    *float64
	}{{"min_free_disk_gb", "BW_MIN_FREE_DISK_GB", 10, c.Resources.MinDisk}, {"min_free_memory_gb", "BW_MIN_FREE_MEMORY_GB", 2, c.Resources.MinMemory}, {"critical_free_disk_gb", "BW_CRITICAL_FREE_DISK_GB", 3, c.Resources.CriticalDisk}, {"critical_free_memory_gb", "BW_CRITICAL_FREE_MEMORY_GB", 0.5, c.Resources.CriticalMemory}} {
		if e := set(v.k, v.e, v.d, v.p); e != nil {
			return r, e
		}
	}
	var maxClaims *float64
	if c.Resources.MaxClaims != nil {
		v := float64(*c.Resources.MaxClaims)
		maxClaims = &v
	}
	if e := set("max_claims", "BW_MAX_CLAIMS", 0, maxClaims); e != nil {
		return r, e
	}
	cap := r.Levels["max_claims"]
	if cap.Value < 0 || cap.Value != math.Trunc(cap.Value) || cap.Source != "default" && cap.Value < 1 {
		return r, fail(1, "BW_MAX_CLAIMS must be a positive integer")
	}
	if os.Getenv("BW_RESOURCE_GUARD") == "off" {
		r.Enabled = false
		r.Reason = "disabled by BW_RESOURCE_GUARD=off"
	} else if !r.Enabled {
		r.Reason = "resource guard unsupported on " + runtime.GOOS
	}
	if !r.Enabled {
		r.Status = "off"
		if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
			r.DiskGB, r.MemoryGB, _ = readResources(root)
		}
		return r, nil
	}
	if r.Levels["critical_free_disk_gb"].Value > r.Levels["min_free_disk_gb"].Value || r.Levels["critical_free_memory_gb"].Value > r.Levels["min_free_memory_gb"].Value {
		return r, fail(1, "effective critical resource levels must not exceed minimums")
	}
	var err error
	r.DiskGB, r.MemoryGB, err = readResources(root)
	if err != nil {
		return r, fail(5, "Cannot read resource levels: %v; BW_RESOURCE_GUARD=off disables the guard", err)
	}
	for _, v := range []struct {
		k        string
		current  float64
		critical bool
	}{{"critical_free_disk_gb", r.DiskGB, true}, {"critical_free_memory_gb", r.MemoryGB, true}, {"min_free_disk_gb", r.DiskGB, false}, {"min_free_memory_gb", r.MemoryGB, false}} {
		level := r.Levels[v.k]
		if v.current < level.Value {
			r.Status = "low_resources"
			if v.critical {
				r.Status = "critical_resources"
			}
			r.Reason = fmt.Sprintf("%s: %.2f GB available, requires %.2f GB; override with %s", v.k, v.current, level.Value, level.Variable)
			return r, nil
		}
	}
	if cap.Value > 0 && float64(r.Claims) >= cap.Value {
		r.Status = "low_resources"
		r.Reason = fmt.Sprintf("max_claims: %d claimed, limit %.0f; override with BW_MAX_CLAIMS", r.Claims, cap.Value)
	}
	return r, nil
}
func guardClaim(c Catalog, s State, root string) error {
	r, e := resourceReport(c, s, root)
	if e != nil {
		return e
	}
	if r.Status == "low_resources" || r.Status == "critical_resources" {
		return fail(5, "%s", r.Reason)
	}
	return nil
}
func resourceText(r ResourceReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Resources: %s; disk %.2f GB; memory %.2f GB; %d claims\n", r.Status, r.DiskGB, r.MemoryGB, r.Claims)
	for _, k := range []string{"min_free_disk_gb", "min_free_memory_gb", "critical_free_disk_gb", "critical_free_memory_gb", "max_claims"} {
		v := r.Levels[k]
		value := fmt.Sprint(v.Value)
		if k == "max_claims" && v.Value == 0 {
			value = "unlimited"
		}
		fmt.Fprintf(&b, "%s: %s (%s; %s)\n", k, value, v.Source, v.Variable)
	}
	b.WriteString(r.Reason)
	return b.String()
}
