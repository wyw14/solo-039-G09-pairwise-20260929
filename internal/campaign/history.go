package campaign

import (
	"github.com/wyw14/solo-039/internal/model"
	"sync"
	"time"
)

type Change struct {
	Before model.Campaign
	After  model.Campaign
	At     time.Time
	Reason string
}

type History struct {
	mu      sync.Mutex
	changes []Change
}

func NewHistory() *History { return &History{changes: make([]Change, 0, 32)} }

func (h *History) Record(before, after model.Campaign, reason string) {
	h.mu.Lock()
	h.changes = append(h.changes, Change{Before: before, After: after, At: time.Now(), Reason: reason})
	h.mu.Unlock()
}

func (h *History) Latest() (Change, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.changes) == 0 {
		return Change{}, false
	}
	return h.changes[len(h.changes)-1], true
}

func (h *History) ForRevision(revision string) []Change {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]Change, 0)
	for _, change := range h.changes {
		if change.After.Revision.String() == revision {
			out = append(out, change)
		}
	}
	return out
}

func (h *History) Phases() []model.Phase {
	h.mu.Lock()
	defer h.mu.Unlock()
	phases := make([]model.Phase, 0, len(h.changes))
	for _, change := range h.changes {
		phases = append(phases, change.After.Phase)
	}
	return phases
}

func (h *History) Count() int { h.mu.Lock(); defer h.mu.Unlock(); return len(h.changes) }

// Changes returns a locked copy of every recorded change, in order.
func (h *History) Changes() []Change {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]Change(nil), h.changes...)
}
