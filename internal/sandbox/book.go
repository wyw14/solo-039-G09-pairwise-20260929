package sandbox

import (
	"github.com/google/uuid"
	"github.com/wyw14/solo-039/internal/interlock"
	"sort"
	"sync"
)

// RuleVersion is one published, immutable rule on a grade's version chain.
// The built-in v1 (Grade == "") is the global baseline. Effective intervals
// are half-open: [EffectiveFrom, EffectiveTo); EffectiveTo == 0 means open.
type RuleVersion struct {
	ID            string
	Grade         string
	Number        int
	Rule          interlock.Rule
	EffectiveFrom int64
	EffectiveTo   int64
	ApprovedBy    string
	PublishedAt   int64
	SourceDraft   uuid.UUID
}

const BuiltinNumber = 1

// RuleBook holds the built-in baseline and one independent version chain per
// grade. Publishing for a grade never touches other grades.
type RuleBook struct {
	mu       sync.RWMutex
	versions []RuleVersion
}

func NewRuleBook() *RuleBook {
	return &RuleBook{versions: []RuleVersion{builtinVersion()}}
}

func builtinVersion() RuleVersion {
	return RuleVersion{
		ID:     "builtin",
		Grade:  "",
		Number: BuiltinNumber,
		Rule:   interlock.DefaultRule(),
	}
}

// Current returns the open version for a grade, falling back to the built-in.
func (b *RuleBook) Current(grade string) RuleVersion {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for i := len(b.versions) - 1; i >= 0; i-- {
		v := b.versions[i]
		if v.Grade == grade && v.EffectiveTo == 0 {
			return v
		}
	}
	return b.versions[0]
}

// VersionAt resolves the rule in force for a grade at a unix-seconds instant,
// which keeps historical decisions explainable. Instants before a grade's
// first publication resolve to the built-in.
func (b *RuleBook) VersionAt(grade string, at int64) RuleVersion {
	b.mu.RLock()
	defer b.mu.RUnlock()
	gradeVersions := make([]RuleVersion, 0, len(b.versions))
	for _, v := range b.versions {
		if v.Grade == grade {
			gradeVersions = append(gradeVersions, v)
		}
	}
	sort.SliceStable(gradeVersions, func(i, j int) bool { return gradeVersions[i].Number < gradeVersions[j].Number })
	for i := len(gradeVersions) - 1; i >= 0; i-- {
		v := gradeVersions[i]
		if v.EffectiveFrom <= at && (v.EffectiveTo == 0 || at < v.EffectiveTo) {
			return v
		}
	}
	return b.versions[0]
}

// Versions returns the built-in followed by the grade's versions in number
// order.
func (b *RuleBook) Versions(grade string) []RuleVersion {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := []RuleVersion{b.versions[0]}
	for _, v := range b.versions {
		if v.Grade == grade {
			out = append(out, v)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Number < out[j].Number })
	return out
}

// Find resolves a version by number; number 1 always returns the built-in.
func (b *RuleBook) Find(number int, grade string) (RuleVersion, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if number == BuiltinNumber {
		return b.versions[0], true
	}
	for _, v := range b.versions {
		if v.Number == number && v.Grade == grade {
			return v, true
		}
	}
	return RuleVersion{}, false
}

// replace swaps the stored versions after a load; a missing built-in is
// re-seeded so the book is never without a baseline.
func (b *RuleBook) replace(versions []RuleVersion) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(versions) == 0 {
		b.versions = []RuleVersion{builtinVersion()}
		return
	}
	b.versions = append([]RuleVersion(nil), versions...)
}

func (b *RuleBook) snapshot() []RuleVersion {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return append([]RuleVersion(nil), b.versions...)
}

// publish closes the grade's open version, appends the successor and returns
// it. The built-in is never modified.
func (b *RuleBook) publish(grade string, rule interlock.Rule, approvedBy string, draft uuid.UUID, now int64) RuleVersion {
	b.mu.Lock()
	defer b.mu.Unlock()
	nextNumber := BuiltinNumber
	for _, v := range b.versions {
		if v.Number >= nextNumber {
			nextNumber = v.Number + 1
		}
	}
	for i := range b.versions {
		if b.versions[i].Grade == grade && b.versions[i].EffectiveTo == 0 {
			b.versions[i].EffectiveTo = now
		}
	}
	version := RuleVersion{
		ID:            uuid.NewString(),
		Grade:         grade,
		Number:        nextNumber,
		Rule:          rule,
		EffectiveFrom: now,
		ApprovedBy:    approvedBy,
		PublishedAt:   now,
		SourceDraft:   draft,
	}
	b.versions = append(b.versions, version)
	return version
}
