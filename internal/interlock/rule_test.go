package interlock

import (
	"github.com/google/uuid"
	"github.com/wyw14/solo-039/internal/model"
	"testing"
)

func onSpecCampaign() model.Campaign {
	return model.Campaign{ID: uuid.New(), Revision: uuid.New(), Phase: model.OnSpec, Epoch: 1, Fencing: 1}
}

func independentProofs(n int) []model.Proof {
	proofs := make([]model.Proof, 0, n)
	for i := 0; i < n; i++ {
		proofs = append(proofs, model.Proof{
			ID:          uuid.New(),
			Revision:    uuid.New(),
			Source:      "source-" + string(rune('a'+i)),
			Digest:      "digest",
			Independent: true,
			At:          int64(i + 1),
		})
	}
	return proofs
}

func TestDefaultRuleRejectsInsufficientEvidence(t *testing.T) {
	permit := DefaultRule().Evaluate(independentProofs(1), onSpecCampaign())
	if permit.Allowed {
		t.Fatal("permit with one proof must be denied")
	}
	if len(permit.Reasons) != 1 || permit.Reasons[0] != "insufficient evidence" {
		t.Fatalf("unexpected reasons: %v", permit.Reasons)
	}
}

func TestDefaultRuleRejectsNonIndependentEvidence(t *testing.T) {
	proofs := independentProofs(2)
	proofs[1].Source = proofs[0].Source
	permit := DefaultRule().Evaluate(proofs, onSpecCampaign())
	if permit.Allowed {
		t.Fatal("permit with duplicate sources must be denied")
	}
	if len(permit.Reasons) != 1 || permit.Reasons[0] != "evidence is not independent" {
		t.Fatalf("unexpected reasons: %v", permit.Reasons)
	}
}

func TestDefaultRuleRejectsNonOnSpecPhase(t *testing.T) {
	campaign := onSpecCampaign()
	campaign.Phase = model.Stabilizing
	permit := DefaultRule().Evaluate(independentProofs(2), campaign)
	if permit.Allowed {
		t.Fatal("permit while stabilizing must be denied")
	}
	if len(permit.Reasons) != 1 || permit.Reasons[0] != "campaign is not on-spec" {
		t.Fatalf("unexpected reasons: %v", permit.Reasons)
	}
}

func TestDefaultRuleAllowsTwoIndependentOnSpecProofs(t *testing.T) {
	permit := DefaultRule().Evaluate(independentProofs(2), onSpecCampaign())
	if !permit.Allowed {
		t.Fatalf("permit must be allowed: %v", permit.Reasons)
	}
	if len(permit.Reasons) != 0 {
		t.Fatalf("allowed permit must have no reasons: %v", permit.Reasons)
	}
}

func TestRuleRejectsMissingRequiredSource(t *testing.T) {
	rule := DefaultRule()
	rule.RequiredSources = []string{"composition", "column-pressure"}
	proofs := independentProofs(2)
	proofs[0].Source = "composition"
	proofs[1].Source = "analyzer"
	permit := rule.Evaluate(proofs, onSpecCampaign())
	if permit.Allowed {
		t.Fatal("permit without column-pressure proof must be denied")
	}
	if len(permit.Reasons) != 1 || permit.Reasons[0] != "required evidence source missing: column-pressure" {
		t.Fatalf("unexpected reasons: %v", permit.Reasons)
	}
}

func TestRuleAllowsAdditionalPhasesWithGenericReason(t *testing.T) {
	rule := Rule{MinimumEvidence: 2, RequireIndependent: true, AllowedPhases: []model.Phase{model.OnSpec, model.Stabilizing}}
	campaign := onSpecCampaign()
	campaign.Phase = model.Stabilizing
	permit := rule.Evaluate(independentProofs(2), campaign)
	if !permit.Allowed {
		t.Fatalf("stabilizing permit must be allowed: %v", permit.Reasons)
	}
	campaign.Phase = model.Warming
	permit = rule.Evaluate(independentProofs(2), campaign)
	if permit.Allowed || permit.Reasons[0] != "campaign phase is not permitted" {
		t.Fatalf("warming permit must use generic reason: %v", permit.Reasons)
	}
}

func TestRuleAllowsSingleProofWhenMinimumIsOne(t *testing.T) {
	rule := Rule{MinimumEvidence: 1, RequireIndependent: true, AllowedPhases: []model.Phase{model.OnSpec}}
	permit := rule.Evaluate(independentProofs(1), onSpecCampaign())
	if !permit.Allowed {
		t.Fatalf("single proof must satisfy minimum of one: %v", permit.Reasons)
	}
}

func TestRuleEqual(t *testing.T) {
	if !DefaultRule().Equal(DefaultRule()) {
		t.Fatal("default rules must be equal")
	}
	a := Rule{MinimumEvidence: 3, RequireIndependent: false, AllowedPhases: []model.Phase{model.Stabilizing, model.OnSpec}, RequiredSources: []string{"b", "a"}}
	b := Rule{MinimumEvidence: 3, RequireIndependent: false, AllowedPhases: []model.Phase{model.OnSpec, model.Stabilizing}, RequiredSources: []string{"a", "b"}}
	if !a.Equal(b) {
		t.Fatal("rules with reordered phases/sources must be equal")
	}
	c := b
	c.MinimumEvidence = 2
	if a.Equal(c) {
		t.Fatal("rules with different minimum must not be equal")
	}
}

func TestCoordinatorDelegatesToDefaultRule(t *testing.T) {
	coordinator := NewCoordinator()
	if !coordinator.Rule.Equal(DefaultRule()) {
		t.Fatal("new coordinator must carry the default rule")
	}
	allowed := coordinator.Evaluate(independentProofs(2), onSpecCampaign())
	if !allowed.Allowed {
		t.Fatalf("coordinator must delegate evaluation to its rule: %v", allowed.Reasons)
	}
}
