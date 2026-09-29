package interlock

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/wyw14/solo-039/internal/model"
)

func newSeededBook(t *testing.T, at time.Time) *RuleBook {
	t.Helper()
	book := NewRuleBook()
	if err := book.Seed(book.Bootstrap(at)); err != nil {
		t.Fatalf("seed baseline: %v", err)
	}
	return book
}

func proof(source string) model.Proof {
	return model.Proof{ID: uuid.New(), Revision: uuid.New(), Source: source, Digest: "d-" + source, Durable: true, Independent: true, At: 1}
}

func onSpecCampaign(grade string) model.Campaign {
	return model.Campaign{ID: uuid.New(), Revision: uuid.New(), Grade: grade, Phase: model.OnSpec, Epoch: 1, Fencing: 1}
}

func TestRuleBookBaselineMatchesLegacyCoordinator(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	book := newSeededBook(t, now)
	engine := NewRuleEngine()

	rule, ok := book.ActiveAt("polymer-grade", now)
	if !ok {
		t.Fatal("expected baseline rule for unknown grade via global fallback")
	}
	if rule.Version != 1 {
		t.Fatalf("baseline version = %d, want 1", rule.Version)
	}
	campaign := onSpecCampaign("polymer-grade")
	if permit := engine.Evaluate(rule, DecisionInput{Proofs: []model.Proof{proof("analyzer"), proof("column-pressure")}, Campaign: campaign}); !permit.Allowed {
		t.Fatalf("two independent proofs on-spec should be allowed: %v", permit.Reasons)
	}
	if permit := engine.Evaluate(rule, DecisionInput{Proofs: []model.Proof{proof("analyzer")}, Campaign: campaign}); permit.Allowed {
		t.Fatal("single proof must be denied")
	}
	duplicate := proof("analyzer")
	duplicate.Source = "analyzer"
	if permit := engine.Evaluate(rule, DecisionInput{Proofs: []model.Proof{proof("analyzer"), duplicate}, Campaign: campaign}); permit.Allowed {
		t.Fatal("same-source proofs must be denied")
	}
}

func TestRuleBookFutureVersionDoesNotActivateEarly(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	book := newSeededBook(t, now)
	later, err := book.Publish(Rule{
		Grade:       "tight-grade",
		Criteria:    RuleCriteria{MinimumEvidence: 3, RequireIndependent: true, AllowedPhases: []model.Phase{model.OnSpec}},
		EffectiveAt: now.Add(time.Hour).Unix(),
		ApprovedBy:  "reviewer",
	}, now)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if rule, ok := book.ActiveAt("tight-grade", now); !ok || rule.Version != 1 {
		t.Fatalf("before effective time must fall back to global baseline v1, got ok=%v", ok)
	}
	rule, ok := book.ActiveAt("tight-grade", now.Add(time.Hour))
	if !ok || rule.Version != later.Version {
		t.Fatalf("expected version %d active after effective time, got ok=%v", later.Version, ok)
	}
	// 旧版本仍保留在簿中可按版本取回，供历史解释。
	old, ok := book.Version(1)
	if !ok || old.Version != 1 {
		t.Fatal("baseline version must remain retrievable")
	}
}

func TestPendingDraftNeverTouchesOnlineJudgement(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	book := newSeededBook(t, now)
	engine := NewRuleEngine()
	log := NewDecisionLog()
	store := NewDraftStore(book, engine)
	judge := NewOnlineJudge(book, engine, log)

	draft, err := store.Submit(DraftInput{
		Grade:     "tight-grade",
		Criteria:  RuleCriteria{MinimumEvidence: 3, RequireIndependent: true, AllowedPhases: []model.Phase{model.OnSpec}},
		ExpiresAt: now.Add(time.Hour).Unix(),
		Rationale: "require triple evidence",
		CreatedBy: "engineer-a",
	}, now)
	if err != nil {
		t.Fatalf("submit draft: %v", err)
	}

	campaign := onSpecCampaign("tight-grade")
	proofs := []model.Proof{proof("analyzer"), proof("column-pressure")}
	// 草案未批准：在线判定仍按基线规则放行，且规则簿中不存在该品级的版本。
	permit, decision, err := judge.Decide("route-product", DecisionInput{Proofs: proofs, Campaign: campaign, At: now})
	if err != nil || !permit.Allowed {
		t.Fatalf("pending draft altered online judgement: err=%v reasons=%v", err, permit.Reasons)
	}
	if decision.RuleVersion != 1 {
		t.Fatalf("decision version = %d, want baseline 1", decision.RuleVersion)
	}
	if versions := book.Versions(); len(versions) != 1 {
		t.Fatalf("rule book must keep only baseline while draft pending, got %d versions", len(versions))
	}

	// 回放：候选规则对同一批证据应收紧（旧放行、新拒绝）。
	summary, err := store.Replay(draft.ID, []BatchEvidence{{
		BatchID:   "batch-1",
		Grade:     "tight-grade",
		Phase:     model.OnSpec,
		Proofs:    proofs,
		DecidedAt: now.Unix(),
	}}, now)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if summary.Batches != 1 || summary.Changed != 1 || summary.Tightened != 1 || summary.Loosened != 0 {
		t.Fatalf("unexpected replay summary: %+v", summary)
	}
	result := summary.Results[0]
	if !result.OldAllowed || result.NewAllowed || result.OldVersion != 1 {
		t.Fatalf("unexpected batch replay: %+v", result)
	}
}

func TestApprovalPublishesAndActivatesNewVersion(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	book := newSeededBook(t, now)
	engine := NewRuleEngine()
	log := NewDecisionLog()
	store := NewDraftStore(book, engine)
	judge := NewOnlineJudge(book, engine, log)

	draft, err := store.Submit(DraftInput{
		Grade:     "tight-grade",
		Criteria:  RuleCriteria{MinimumEvidence: 3, RequireIndependent: true, AllowedPhases: []model.Phase{model.OnSpec}},
		ExpiresAt: now.Add(time.Hour).Unix(),
		CreatedBy: "engineer-a",
	}, now)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	published, err := store.Approve(draft.ID, "shift-supervisor", now)
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	if published.Status != DraftPublished || published.PublishedVersion != 2 {
		t.Fatalf("unexpected published draft: %+v", published)
	}

	campaign := onSpecCampaign("tight-grade")
	two := []model.Proof{proof("analyzer"), proof("column-pressure")}
	if _, _, err := judge.Decide("route-product", DecisionInput{Proofs: two, Campaign: campaign, At: now}); err == nil {
		t.Fatal("new rule must require three proofs after publish")
	}
	three := []model.Proof{proof("analyzer"), proof("column-pressure"), proof("reflux")}
	if _, _, err := judge.Decide("route-product", DecisionInput{Proofs: three, Campaign: campaign, At: now}); err != nil {
		t.Fatalf("three independent proofs should be allowed under v2: %v", err)
	}

	// 已发布的草案不能重复发布。
	if _, err := store.Approve(draft.ID, "someone-else", now); err == nil {
		t.Fatal("published draft must not be approvable again")
	}
}

func TestExpiredDraftCannotBeApproved(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	book := newSeededBook(t, now)
	store := NewDraftStore(book, NewRuleEngine())
	draft, err := store.Submit(DraftInput{
		Grade:     "g",
		Criteria:  RuleCriteria{MinimumEvidence: 1, RequireIndependent: true},
		ExpiresAt: now.Add(time.Minute).Unix(),
		CreatedBy: "engineer-a",
	}, now)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	after := now.Add(2 * time.Minute)
	if swept := store.Sweep(after); swept != 1 {
		t.Fatalf("sweep expired count = %d, want 1", swept)
	}
	stored, _ := store.Get(draft.ID)
	if stored.Status != DraftExpired {
		t.Fatalf("status = %s, want expired", stored.Status)
	}
	if _, err := store.Approve(draft.ID, "reviewer", after); err == nil {
		t.Fatal("expired draft must not be approvable")
	}
	if versions := book.Versions(); len(versions) != 1 {
		t.Fatalf("expired draft must not publish a version, got %d", len(versions))
	}
}

func TestRejectedDraftStaysInert(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	book := newSeededBook(t, now)
	store := NewDraftStore(book, NewRuleEngine())
	draft, err := store.Submit(DraftInput{
		Grade:     "g",
		Criteria:  RuleCriteria{MinimumEvidence: 1, AllowedPhases: []model.Phase{model.OnSpec}},
		ExpiresAt: now.Add(time.Hour).Unix(),
		CreatedBy: "engineer-a",
	}, now)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	rejected, err := store.Reject(draft.ID, "reviewer", "needs reflux evidence", now)
	if err != nil || rejected.Status != DraftRejected {
		t.Fatalf("reject failed: draft=%+v err=%v", rejected, err)
	}
	if _, err := store.Approve(draft.ID, "reviewer", now); err == nil {
		t.Fatal("rejected draft must not be approvable")
	}
	if versions := book.Versions(); len(versions) != 1 {
		t.Fatal("rejected draft must not reach the rule book")
	}
}

func TestHistoryIsExplainedWithRuleAtDecisionTime(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	book := newSeededBook(t, now)
	engine := NewRuleEngine()
	log := NewDecisionLog()
	store := NewDraftStore(book, engine)
	judge := NewOnlineJudge(book, engine, log)

	campaign := onSpecCampaign("tight-grade")
	two := []model.Proof{proof("analyzer"), proof("column-pressure")}
	_, historical, err := judge.Decide("route-product", DecisionInput{Proofs: two, Campaign: campaign, At: now})
	if err != nil {
		t.Fatalf("baseline decision: %v", err)
	}

	// 发布更严格的新版本。
	draft, err := store.Submit(DraftInput{
		Grade:     "tight-grade",
		Criteria:  RuleCriteria{MinimumEvidence: 3, RequireIndependent: true, AllowedPhases: []model.Phase{model.OnSpec}},
		ExpiresAt: now.Add(time.Hour).Unix(),
		CreatedBy: "engineer-a",
	}, now)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if _, err := store.Approve(draft.ID, "reviewer", now); err != nil {
		t.Fatalf("approve: %v", err)
	}

	// 用当前规则重放旧证据会被拒绝……
	currentRule, _ := book.ActiveAt("tight-grade", now)
	if permit := engine.Evaluate(currentRule, DecisionInput{Proofs: two, Campaign: campaign}); permit.Allowed {
		t.Fatal("current rule should deny the old two-proof batch")
	}
	// ……但历史解释必须取当时保存的 v1，结论仍是放行。
	explanation, err := HistoricalExplanation(book, engine, historical)
	if err != nil {
		t.Fatalf("historical explanation: %v", err)
	}
	if !explanation.Allowed {
		t.Fatalf("historical decision must remain allowed under v1: %v", explanation.Reasons)
	}
}

func TestSubmitRejectsBlankCriteriaAndPastExpiry(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	store := NewDraftStore(newSeededBook(t, now), NewRuleEngine())
	if _, err := store.Submit(DraftInput{Grade: "g", Criteria: RuleCriteria{}, ExpiresAt: now.Add(time.Hour).Unix()}, now); err == nil {
		t.Fatal("empty criteria must be rejected")
	}
	if _, err := store.Submit(DraftInput{
		Grade:     "g",
		Criteria:  RuleCriteria{MinimumEvidence: 2, AllowedPhases: []model.Phase{model.OnSpec}},
		ExpiresAt: now.Add(-time.Minute).Unix(),
	}, now); err == nil {
		t.Fatal("past expiry must be rejected")
	}
	if _, err := store.Submit(DraftInput{
		Grade:     "g",
		Criteria:  RuleCriteria{MinimumEvidence: 2, RequiredSources: []string{"a", "a"}},
		ExpiresAt: now.Add(time.Hour).Unix(),
	}, now); err == nil {
		t.Fatal("duplicate required sources must be rejected")
	}
}

func TestBoundCoordinatorUsesPublishedVersions(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	book := newSeededBook(t, now)
	coordinator := NewCoordinator()
	coordinator.BindRuleBook(book)
	store := NewDraftStore(book, NewRuleEngine())

	campaign := onSpecCampaign("g")
	two := []model.Proof{proof("a"), proof("b")}
	if !coordinator.Evaluate(two, campaign).Allowed {
		t.Fatal("baseline should allow two proofs")
	}
	draft, _ := store.Submit(DraftInput{
		Grade:     "g",
		Criteria:  RuleCriteria{MinimumEvidence: 3, RequireIndependent: true, AllowedPhases: []model.Phase{model.OnSpec}},
		ExpiresAt: now.Add(time.Hour).Unix(),
		CreatedBy: "x",
	}, now)
	if _, err := store.Approve(draft.ID, "reviewer", now); err != nil {
		t.Fatalf("approve: %v", err)
	}
	if coordinator.Evaluate(two, campaign).Allowed {
		t.Fatal("bound coordinator must enforce published v2")
	}
}
