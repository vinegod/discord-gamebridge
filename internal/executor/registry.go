package executor

import "fmt"

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
				_ = fmt.Errorf("executor %q close: %w", name, err)
			}
		}
	}
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
