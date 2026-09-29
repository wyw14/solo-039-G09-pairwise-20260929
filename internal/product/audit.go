package product

import (
	"errors"
	"github.com/wyw14/solo-039/internal/model"
	"sync"
	"time"
)

type Decision struct {
	Plan        model.ProductPlan
	Proof       model.Proof
	Opened      bool
	At          time.Time
	Reason      string
	RuleVersion int
}

type Audit struct {
	mu        sync.Mutex
	decisions []Decision
}

func NewAudit() *Audit { return &Audit{decisions: make([]Decision, 0, 32)} }

func (a *Audit) Record(plan model.ProductPlan, proof model.Proof, opened bool, reason string, ruleVersion int) {
	a.mu.Lock()
	a.decisions = append(a.decisions, Decision{Plan: plan, Proof: proof, Opened: opened, At: time.Now(), Reason: reason, RuleVersion: ruleVersion})
	a.mu.Unlock()
}

func (a *Audit) Latest() (Decision, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.decisions) == 0 {
		return Decision{}, errors.New("no product decisions")
	}
	return a.decisions[len(a.decisions)-1], nil
}

func (a *Audit) ForRevision(revision string) []Decision {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]Decision, 0)
	for _, decision := range a.decisions {
		if decision.Plan.Revision.String() == revision {
			out = append(out, decision)
		}
	}
	return out
}

func (a *Audit) Count() int { a.mu.Lock(); defer a.mu.Unlock(); return len(a.decisions) }
