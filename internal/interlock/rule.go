package interlock

import (
	"github.com/wyw14/solo-039/internal/model"
	"sort"
)

// Rule is a pure, versionable interlock permit criterion. The default rule
// (DefaultRule) reproduces the historical Coordinator behaviour byte-for-byte;
// draft rules live in the sandbox and never touch online evaluation until an
// approved draft is published through the rule book.
type Rule struct {
	MinimumEvidence    int
	RequireIndependent bool
	AllowedPhases      []model.Phase
	RequiredSources    []string
}

// DefaultRule returns the built-in v1 permit criterion: at least two pieces of
// evidence, mutually independent sources, and the campaign on-spec.
func DefaultRule() Rule {
	return Rule{
		MinimumEvidence:    2,
		RequireIndependent: true,
		AllowedPhases:      []model.Phase{model.OnSpec},
	}
}

func (r Rule) Evaluate(proofs []model.Proof, campaign model.Campaign) Permit {
	p := Permit{Evidence: append([]model.Proof(nil), proofs...)}
	if len(proofs) < r.MinimumEvidence {
		p.Reasons = append(p.Reasons, "insufficient evidence")
		return p
	}
	if missing := r.missingSource(proofs); missing != "" {
		p.Reasons = append(p.Reasons, "required evidence source missing: "+missing)
		return p
	}
	if r.RequireIndependent && !Independent(proofs) {
		p.Reasons = append(p.Reasons, "evidence is not independent")
		return p
	}
	if !r.allowsPhase(campaign.Phase) {
		p.Reasons = append(p.Reasons, r.phaseReason())
		return p
	}
	p.Allowed = true
	return p
}

func (r Rule) allowsPhase(phase model.Phase) bool {
	for _, allowed := range r.AllowedPhases {
		if allowed == phase {
			return true
		}
	}
	return false
}

// phaseReason keeps the historical wording for the built-in on-spec-only rule
// and uses a generic message for any other phase set.
func (r Rule) phaseReason() string {
	if len(r.AllowedPhases) == 1 && r.AllowedPhases[0] == model.OnSpec {
		return "campaign is not on-spec"
	}
	return "campaign phase is not permitted"
}

func (r Rule) missingSource(proofs []model.Proof) string {
	if len(r.RequiredSources) == 0 {
		return ""
	}
	present := make(map[string]struct{}, len(proofs))
	for _, proof := range proofs {
		present[proof.Source] = struct{}{}
	}
	for _, source := range r.RequiredSources {
		if _, ok := present[source]; !ok {
			return source
		}
	}
	return ""
}

// Equal compares two rules by value, treating phase/source slices as sets
// compared in sorted order.
func (r Rule) Equal(o Rule) bool {
	if r.MinimumEvidence != o.MinimumEvidence || r.RequireIndependent != o.RequireIndependent {
		return false
	}
	return sameStringSet(phasesAsStrings(r.AllowedPhases), phasesAsStrings(o.AllowedPhases)) &&
		sameStringSet(append([]string(nil), r.RequiredSources...), append([]string(nil), o.RequiredSources...))
}

func phasesAsStrings(phases []model.Phase) []string {
	out := make([]string, len(phases))
	for i, phase := range phases {
		out[i] = string(phase)
	}
	return out
}

func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	a = append([]string(nil), a...)
	b = append([]string(nil), b...)
	sort.Strings(a)
	sort.Strings(b)
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
