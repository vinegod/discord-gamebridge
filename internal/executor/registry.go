package executor

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
)

// ExecutorStatus reports the health of a single executor.
// HasCheck is false for executors that do not implement HealthChecker.
type ExecutorStatus struct {
	Healthy  bool
	HasCheck bool
}

// Registry holds named Executor instances built from config.
type Registry struct {
	executors map[string]Executor
}

// NewRegistry creates an empty Registry.
func NewRegistry() *Registry {
	return &Registry{executors: make(map[string]Executor)}
}

// Register adds an executor under the given name.
// Panics on duplicate names.
func (r *Registry) Register(name string, e Executor) {
	if _, exists := r.executors[name]; exists {
		panic(fmt.Sprintf("executor %q already registered", name))
	}
	r.executors[name] = e
}

// Get returns the executor registered under name, or an error if not found.
func (r *Registry) Get(name string) (Executor, error) {
	e, ok := r.executors[name]
	if !ok {
		return nil, fmt.Errorf("executor %q not found — check your config", name)
	}
	return e, nil
}

// CloseAll calls Close on every executor that implements LifecycleExecutor.
func (r *Registry) CloseAll() {
	for name, e := range r.executors {
		if lc, ok := e.(LifecycleExecutor); ok {
			if err := lc.Close(); err != nil {
				slog.Error("executor close failed", "name", name, "error", err)
			}
		}
	}
}

// Statuses runs health checks on all registered executors concurrently and returns a
// name→status map. Executors that do not implement HealthChecker report HasCheck=false.
func (r *Registry) Statuses(ctx context.Context) map[string]ExecutorStatus {
	result := make(map[string]ExecutorStatus, len(r.executors))
	var mu sync.Mutex
	var wg sync.WaitGroup

	for name, e := range r.executors {
		hc, ok := e.(HealthChecker)
		if !ok {
			result[name] = ExecutorStatus{HasCheck: false}
			continue
		}
		wg.Add(1)
		go func(n string, h HealthChecker) {
			defer wg.Done()
			healthy := h.Healthy(ctx)
			mu.Lock()
			result[n] = ExecutorStatus{Healthy: healthy, HasCheck: true}
			mu.Unlock()
		}(name, hc)
	}
	wg.Wait()
	return result
}

// ValidateNames returns an error for every executor name in names that is not registered.
func (r *Registry) ValidateNames(names []string) error {
	var missing []string
	for _, name := range names {
		if _, ok := r.executors[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("unknown executor(s) referenced in config: %v", missing)
	}
	return nil
}
