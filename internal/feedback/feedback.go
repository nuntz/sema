// Package feedback owns the Keep, Boost and Bury rules. Storage executes the
// returned plan, preserving its conditional writes and retry semantics.
package feedback

import (
	"errors"

	"github.com/nuntz/sema/internal/domain"
)

var ErrKept = errors.New("kept items cannot be buried")
var ErrValue = errors.New("value must be -1, 0, or 1")

type Action string

const (
	Signal Action = "signal"
	Keep   Action = "keep"
	Unkeep Action = "unkeep"
)

type State struct {
	Kept   bool
	Value  int
	Source string
}
type Plan struct {
	Kept     bool
	Value    int
	Source   string
	Preserve bool
}

// DeletesSignal reports whether executing the plan removes stored feedback.
func (p Plan) DeletesSignal() bool { return p.Value == 0 && !p.Preserve }

func Kept(item domain.Item) bool {
	return item.ArchiveSK != "" || item.HeartedTS != "" || item.Hearted || domain.IsArchive(item)
}

func Apply(state State, action Action, value int) (Plan, error) {
	switch action {
	case Keep:
		if state.Value > 0 {
			return Plan{Kept: true, Value: state.Value, Source: state.Source, Preserve: true}, nil
		}
		return Plan{Kept: true, Value: 1, Source: "heart"}, nil
	case Unkeep:
		return Plan{}, nil
	case Signal:
		if value < -1 || value > 1 {
			return Plan{}, ErrValue
		}
		if state.Kept && value == -1 {
			return Plan{}, ErrKept
		}
		if state.Kept && value == 0 {
			return Plan{Kept: true, Value: 1, Source: "heart"}, nil
		}
		return Plan{Kept: state.Kept, Value: value}, nil
	default:
		return Plan{}, errors.New("unknown feedback action")
	}
}
