package sandbox

import (
	"github.com/google/uuid"
	"github.com/wyw14/solo-039/internal/interlock"
	"github.com/wyw14/solo-039/internal/model"
	"sort"
)

// Outcome is the permit result under one rule.
type Outcome struct {
	Allowed bool
	Reasons []string
}

// ReplayCase compares the baseline and candidate rules for one evidence batch
// evaluated at one applicable phase.
type ReplayCase struct {
	Revision      string
	Grade         string
	Phase         model.Phase
	EvidenceCount int
	Old           Outcome
	New           Outcome
	Changed       bool
	Direction     string
}

// ReplayReport is the snapshot of a replay run. BaseVersion is filled by the
// Box from the rule book.
type ReplayReport struct {
	Cases       []ReplayCase
	BaseVersion int
	Batches     int
	RanAt       int64
	AllowToDeny int
	DenyToAllow int
	Unchanged   int
}

// EvidenceBatch is one existing campaign revision's evidence set.
type EvidenceBatch struct {
	Revision string
	Grade    string
	Proofs   []model.Proof
}

// BatchSource supplies historical batches for replay. The service layer
// adapts the evidence catalog and campaign history to this interface.
type BatchSource interface {
	Batches() []EvidenceBatch
}

const (
	DirectionUnchanged   = "unchanged"
	DirectionAllowToDeny = "allow->deny"
	DirectionDenyToAllow = "deny->allow"
)

// RunReplay evaluates both rules deterministically over every batch and every
// phase in the union of the two rules' AllowedPhases (model phase order).
func RunReplay(base, candidate interlock.Rule, batches []EvidenceBatch, now int64) ReplayReport {
	ordered := append([]EvidenceBatch(nil), batches...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Grade != ordered[j].Grade {
			return ordered[i].Grade < ordered[j].Grade
		}
		return ordered[i].Revision < ordered[j].Revision
	})
	phases := unionPhases(base.AllowedPhases, candidate.AllowedPhases)

	report := ReplayReport{Cases: make([]ReplayCase, 0, len(ordered)*len(phases)), Batches: len(ordered), RanAt: now}
	for _, batch := range ordered {
		revision, _ := uuid.Parse(batch.Revision)
		for _, phase := range phases {
			campaign := model.Campaign{Revision: revision, Grade: batch.Grade, Phase: phase}
			oldPermit := base.Evaluate(batch.Proofs, campaign)
			newPermit := candidate.Evaluate(batch.Proofs, campaign)
			old := Outcome{Allowed: oldPermit.Allowed, Reasons: append([]string(nil), oldPermit.Reasons...)}
			new := Outcome{Allowed: newPermit.Allowed, Reasons: append([]string(nil), newPermit.Reasons...)}
			direction := DirectionUnchanged
			if old.Allowed && !new.Allowed {
				direction = DirectionAllowToDeny
				report.AllowToDeny++
			} else if !old.Allowed && new.Allowed {
				direction = DirectionDenyToAllow
				report.DenyToAllow++
			} else {
				report.Unchanged++
			}
			report.Cases = append(report.Cases, ReplayCase{
				Revision:      batch.Revision,
				Grade:         batch.Grade,
				Phase:         phase,
				EvidenceCount: len(batch.Proofs),
				Old:           old,
				New:           new,
				Changed:       direction != DirectionUnchanged,
				Direction:     direction,
			})
		}
	}
	return report
}

func unionPhases(a, b []model.Phase) []model.Phase {
	allowed := make(map[model.Phase]struct{}, len(a)+len(b))
	for _, phase := range a {
		allowed[phase] = struct{}{}
	}
	for _, phase := range b {
		allowed[phase] = struct{}{}
	}
	out := make([]model.Phase, 0, len(allowed))
	for _, phase := range model.Phases() {
		if _, ok := allowed[phase]; ok {
			out = append(out, phase)
		}
	}
	return out
}
