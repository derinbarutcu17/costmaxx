package reducers

import (
	"sort"

	"github.com/derinbarutcu17/costmaxx/internal/artifacts"
	"github.com/derinbarutcu17/costmaxx/internal/config"
	"github.com/derinbarutcu17/costmaxx/internal/events"
	"github.com/derinbarutcu17/costmaxx/internal/reducers/build"
	"github.com/derinbarutcu17/costmaxx/internal/reducers/diff"
	"github.com/derinbarutcu17/costmaxx/internal/reducers/generic"
	"github.com/derinbarutcu17/costmaxx/internal/reducers/json"
	"github.com/derinbarutcu17/costmaxx/internal/reducers/lint"
	"github.com/derinbarutcu17/costmaxx/internal/reducers/search"
	"github.com/derinbarutcu17/costmaxx/internal/reducers/terminal"
	"github.com/derinbarutcu17/costmaxx/internal/reducers/tests"
)

type Registry struct {
	reducers []artifacts.Reducer
	cfg      *config.Config
}

func NewRegistry(cfg *config.Config) *Registry {
	r := &Registry{cfg: cfg}
	r.register(tests.New())
	r.register(build.New())
	r.register(terminal.New())
	r.register(diff.New())
	r.register(search.New())
	r.register(lint.New())
	r.register(json.New())
	r.register(generic.New())
	return r
}

func (r *Registry) register(red artifacts.Reducer) {
	r.reducers = append(r.reducers, red)
}

func (r *Registry) Select(category events.OutputCategory, command string, exitCode int, size int64) artifacts.Reducer {
	type scored struct {
		reducer artifacts.Reducer
		score   float64
	}
	var candidates []scored

	for _, red := range r.reducers {
		score := red.CanHandle(string(category), command, exitCode, size)
		if score > 0 {
			candidates = append(candidates, scored{red, score})
		}
	}

	if len(candidates) == 0 {
		return nil
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	return candidates[0].reducer
}

func (r *Registry) All() []artifacts.Reducer {
	return r.reducers
}
