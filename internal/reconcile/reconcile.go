// Package reconcile collects read-only evidence. It never treats evidence as
// agent intent, changes topic lifecycle, or runs commands received from agents.
package reconcile

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type Observation struct {
	Source     string            `json:"source"`
	Kind       string            `json:"kind"`
	Key        string            `json:"key"`
	State      string            `json:"state"`
	Revision   string            `json:"revision,omitempty"`
	Data       map[string]string `json:"data,omitempty"`
	ObservedAt time.Time         `json:"observed_at"`
}
type Collector interface {
	Collect(context.Context) ([]Observation, error)
}
type Source struct {
	Name      string
	Collector Collector
}
type Result struct {
	Source       string        `json:"source"`
	Observations []Observation `json:"observations"`
	Error        string        `json:"error,omitempty"`
	CheckedAt    time.Time     `json:"checked_at"`
}

// Runner runs sources serially so periodic runs cannot overlap. Sink must retain
// previous successful evidence on failures; an empty/failed result isn't deletion.
type Runner struct {
	Sources []Source
	Sink    func(context.Context, Result) error
	Timeout time.Duration
	Now     func() time.Time
}

func (r Runner) Once(ctx context.Context) ([]Result, error) {
	seen := map[string]bool{}
	for _, s := range r.Sources {
		if s.Name == "" || s.Collector == nil || seen[s.Name] {
			return nil, errors.New("sources require unique nonempty names and collectors")
		}
		seen[s.Name] = true
	}
	results := make([]Result, 0, len(r.Sources))
	var errs []error
	for _, s := range r.Sources {
		if err := ctx.Err(); err != nil {
			return results, errors.Join(append(errs, err)...)
		}
		timeout := r.Timeout
		if timeout <= 0 {
			timeout = 30 * time.Second
		}
		child, cancel := context.WithTimeout(ctx, timeout)
		obs, err := s.Collector.Collect(child)
		cancel()
		result := Result{Source: s.Name, Observations: obs, CheckedAt: now(r.Now)}
		if err != nil {
			result.Error = err.Error()
			errs = append(errs, fmt.Errorf("%s: %w", s.Name, err))
		}
		results = append(results, result)
		if r.Sink != nil {
			if err := r.Sink(ctx, result); err != nil {
				errs = append(errs, fmt.Errorf("persist %s: %w", s.Name, err))
			}
		}
	}
	return results, errors.Join(errs...)
}

// Watch runs immediately and then waits interval AFTER each completed run.
// Collection errors are delivered to Sink and retried on the next run. A Sink is
// required so failures cannot disappear silently. Cancellation ends the loop.
func (r Runner) Watch(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		return errors.New("interval must be positive")
	}
	if r.Sink == nil {
		return errors.New("watch requires a result sink")
	}
	// Validate configuration before the loop.
	seen := map[string]bool{}
	for _, s := range r.Sources {
		if s.Name == "" || s.Collector == nil || seen[s.Name] {
			return errors.New("invalid sources")
		}
		seen[s.Name] = true
	}
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var sinkErr error
		run := r
		run.Sink = func(ctx context.Context, result Result) error {
			err := r.Sink(ctx, result)
			if err != nil {
				sinkErr = errors.Join(sinkErr, err)
			}
			return err
		}
		_, _ = run.Once(ctx)
		if sinkErr != nil {
			return fmt.Errorf("persist reconciliation: %w", sinkErr)
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
func now(f func() time.Time) time.Time {
	if f != nil {
		return f().UTC()
	}
	return time.Now().UTC()
}
