package bw

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func waitDurations(o options) (time.Duration, time.Duration, error) {
	timeout, interval := 9*time.Minute, 30*time.Second
	var e error
	if o.timeout != "" {
		timeout, e = time.ParseDuration(o.timeout)
		if e != nil || timeout <= 0 || timeout > time.Hour {
			return 0, 0, fail(1, "--timeout must be positive and at most 60m")
		}
	}
	if o.interval != "" {
		interval, e = time.ParseDuration(o.interval)
		if e != nil || interval < 10*time.Second {
			return 0, 0, fail(1, "--interval must be at least 10s")
		}
	}
	return timeout, interval, nil
}
func executeWait(o options, root string) (outcome, error) {
	who, err := identity(o, root)
	if err != nil {
		return outcome{}, err
	}
	o.as = who
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	timeout, interval, _ := waitDurations(o)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return waitAdvice(ctx, o, root, interval)
}
func waitAdvice(ctx context.Context, o options, root string, interval time.Duration) (outcome, error) {
	last := outcome{result: IdleAdvice{Action: "wait", Reason: "Wait ended before the first snapshot completed."}}
	for {
		if ctx.Err() != nil {
			return last, nil
		}
		next := o
		next.command = "idle"
		out, e := executeReadContext(ctx, next, root)
		if ctx.Err() != nil {
			return last, nil
		}
		if e != nil {
			return out, e
		}
		last = out
		if out.result.(IdleAdvice).Action != "wait" {
			return out, nil
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return last, nil
		case <-timer.C:
		}
	}
}
