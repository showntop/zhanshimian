package taskrunner

import (
	"fmt"
	"time"

	"github.com/zhanshimian/server/internal/domain"
)

type RetryBackoff func(attempt int) time.Duration

type Definition struct {
	Type           domain.TaskType
	MaxAttempts    int
	Timeout        time.Duration
	LeaseDuration  time.Duration
	HeartbeatEvery time.Duration
	Concurrency    int
	RetryBackoff   RetryBackoff
}

type Registry struct {
	order    []domain.TaskType
	defs     map[domain.TaskType]Definition
	handlers map[domain.TaskType]Handler
}

func NewRegistry(definitions []Definition, handlers []Handler) (*Registry, error) {
	defs := make(map[domain.TaskType]Definition, len(definitions))
	order := make([]domain.TaskType, 0, len(definitions))
	for _, def := range definitions {
		if err := validateDefinition(def); err != nil {
			return nil, err
		}
		if _, exists := defs[def.Type]; exists {
			return nil, fmt.Errorf("duplicate task definition: %s", def.Type)
		}
		defs[def.Type] = def
		order = append(order, def.Type)
	}

	hs := make(map[domain.TaskType]Handler, len(handlers))
	for _, handler := range handlers {
		if handler == nil {
			return nil, fmt.Errorf("nil handler")
		}
		taskType := handler.Type()
		if _, exists := hs[taskType]; exists {
			return nil, fmt.Errorf("duplicate handler: %s", taskType)
		}
		if _, ok := defs[taskType]; !ok {
			return nil, fmt.Errorf("unregistered handler: %s", taskType)
		}
		hs[taskType] = handler
	}
	for _, taskType := range order {
		if _, ok := hs[taskType]; !ok {
			return nil, fmt.Errorf("missing handler: %s", taskType)
		}
	}
	return &Registry{order: order, defs: defs, handlers: hs}, nil
}

func (r *Registry) Types() []domain.TaskType {
	out := make([]domain.TaskType, len(r.order))
	copy(out, r.order)
	return out
}

func (r *Registry) Definition(taskType domain.TaskType) (Definition, bool) {
	def, ok := r.defs[taskType]
	return def, ok
}

func (r *Registry) Handler(taskType domain.TaskType) (Handler, bool) {
	handler, ok := r.handlers[taskType]
	return handler, ok
}

func ExponentialBackoff(min, max time.Duration) RetryBackoff {
	return func(attempt int) time.Duration {
		if attempt < 1 {
			attempt = 1
		}
		delay := min
		for i := 1; i < attempt; i++ {
			if max > 0 && delay > max/2 {
				return max
			}
			delay *= 2
		}
		if max > 0 && delay > max {
			return max
		}
		return delay
	}
}

func validateDefinition(def Definition) error {
	if def.MaxAttempts <= 0 {
		return fmt.Errorf("max attempts must be > 0")
	}
	if def.HeartbeatEvery <= 0 {
		return fmt.Errorf("heartbeat every must be > 0")
	}
	if def.Timeout <= def.HeartbeatEvery {
		return fmt.Errorf("timeout must be greater than heartbeat every")
	}
	if def.LeaseDuration < 2*def.HeartbeatEvery {
		return fmt.Errorf("lease duration must be at least twice heartbeat every")
	}
	if def.Concurrency <= 0 {
		return fmt.Errorf("concurrency must be > 0")
	}
	if def.RetryBackoff == nil {
		return fmt.Errorf("missing retry backoff")
	}
	return nil
}
