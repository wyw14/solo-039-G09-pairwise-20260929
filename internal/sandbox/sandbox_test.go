package sandbox

import (
	"github.com/google/uuid"
	"github.com/wyw14/solo-039/internal/interlock"
	"github.com/wyw14/solo-039/internal/model"
	"path/filepath"
	"testing"
)

type fakeBatches struct{ batches []EvidenceBatch }

func (f fakeBatches) Batches() []EvidenceBatch { return f.batches }

func validSpec() RuleSpec {
	return RuleSpec{MinimumEvidence: 3, RequireIndependent: true, AllowedPhases: []model.Phase{model.OnSpec}}
}

func twoProofBatch(grade string) EvidenceBatch {
	revision := uuid.New()
	return EvidenceBatch{
		Revision: revision.String(),
		Grade:    grade,
		Proofs: []model.Proof{
			{ID: uuid.New(), Revision: revision, Source: "composition", Digest: "d1", Independent: true, At: 1},
			{ID: uuid.New(), Revision: revision, Source: "column-pressure", Digest: "d2", Independent: true, At: 2},
		},
	}
}

func TestRuleSpecValidate(t *testing.T) {
	if err := validSpec().Validate(); err != nil {
		t.Fatalf("valid spec rejected: %v", err)
	}
	bad := validSpec()
	bad.MinimumEvidence = 0
	if err := bad.Validate(); err == nil {
		t.Fatal("minimum below one must be rejected")
	}
	bad = validSpec()
	bad.AllowedPhases = nil
	if err := bad.Validate(); err == nil {
		t.Fatal("empty phases must be rejected")
	}
	bad = validSpec()
	bad.AllowedPhases = []model.Phase{model.Phase("bogus")}
	if err := bad.Validate(); err == nil {
		t.Fatal("unknown phase must be rejected")
	}
	bad = validSpec()
	bad.AllowedPhases = []model.Phase{model.OnSpec, model.OnSpec}
	if err := bad.Validate(); err == nil {
		t.Fatal("duplicate phase must be rejected")
	}
	bad = validSpec()
	bad.RequiredSources = []string{"a", "a"}
	if err := bad.Validate(); err == nil {
		t.Fatal("duplicate required source must be rejected")
	}
}

func TestCreateDraftValidation(t *testing.T) {
	box := NewBox(NewStore(filepath.Join(t.TempDir(), "rules.json")))
	const now = 1000
	in := DraftInput{Grade: "poly-A", Spec: validSpec(), ExpiresAt: now + 100, Reason: "tighten", CreatedBy: "ops1"}
	draft, err := box.CreateDraft(in, now)
	if err != nil {
		t.Fatalf("create draft: %v", err)
	}
	if draft.Status != StatusDraft || draft.BasedOn != BuiltinNumber {
		t.Fatalf("unexpected draft: %+v", draft)
	}
	expired := in
	expired.ExpiresAt = now
	if _, err := box.CreateDraft(expired, now); err == nil {
		t.Fatal("draft expiring at creation time must be rejected")
	}
	bad := in
	bad.Reason = ""
	if _, err := box.CreateDraft(bad, now); err == nil {
		t.Fatal("draft without reason must be rejected")
	}
}

func TestApproveRequiresReplay(t *testing.T) {
	box := NewBox(NewStore(filepath.Join(t.TempDir(), "rules.json")))
	const now = 1000
	draft, err := box.CreateDraft(DraftInput{Grade: "poly-A", Spec: validSpec(), ExpiresAt: now + 100, Reason: "r", CreatedBy: "ops"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := box.Approve(draft.ID, "lead", "ok", now+10); err == nil {
		t.Fatal("approval without replay must be rejected")
	}
}

func TestApproveRejectsExpiredDraft(t *testing.T) {
	box := NewBox(NewStore(filepath.Join(t.TempDir(), "rules.json")))
	const now = 1000
	draft, err := box.CreateDraft(DraftInput{Grade: "poly-A", Spec: validSpec(), ExpiresAt: now + 100, Reason: "r", CreatedBy: "ops"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := box.RunReplay(draft.ID, fakeBatches{[]EvidenceBatch{twoProofBatch("poly-A")}}, now+10); err != nil {
		t.Fatal(err)
	}
	if _, err := box.Approve(draft.ID, "lead", "ok", now+200); err == nil {
		t.Fatal("approval past expiry must be rejected")
	}
}

func TestApproveRejectsZeroBatchReplay(t *testing.T) {
	box := NewBox(NewStore(filepath.Join(t.TempDir(), "rules.json")))
	const now = 1000
	draft, err := box.CreateDraft(DraftInput{Grade: "poly-A", Spec: validSpec(), ExpiresAt: now + 100, Reason: "r", CreatedBy: "ops"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := box.RunReplay(draft.ID, fakeBatches{}, now+10); err != nil {
		t.Fatal(err)
	}
	if _, err := box.Approve(draft.ID, "lead", "ok", now+20); err == nil {
		t.Fatal("approval without any replayed batch must be rejected")
	}
}

func TestReplayDetectsAllowToDenyAndUnchanged(t *testing.T) {
	batches := []EvidenceBatch{twoProofBatch("poly-A")}
	report := RunReplay(interlock.DefaultRule(), validSpec().Rule(), batches, 50)
	if report.Batches != 1 || len(report.Cases) != 1 {
		t.Fatalf("expected one case, got %d", len(report.Cases))
	}
	c := report.Cases[0]
	if !c.Old.Allowed || c.New.Allowed || c.Direction != DirectionAllowToDeny || !c.Changed {
		t.Fatalf("expected allow->deny, got old=%v new=%v dir=%s", c.Old.Allowed, c.New.Allowed, c.Direction)
	}
	report = RunReplay(interlock.DefaultRule(), interlock.DefaultRule(), batches, 50)
	if report.Unchanged != 1 || report.AllowToDeny != 0 || report.DenyToAllow != 0 {
		t.Fatalf("identical rules must report unchanged: %+v", report)
	}
}

func TestOpenAndApprovedDraftsDoNotChangeLiveRule(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "rules.json"))
	box := NewBox(store)
	const now = 1000
	if got := box.Book().Current("poly-A").Number; got != BuiltinNumber {
		t.Fatalf("fresh book must resolve to builtin, got %d", got)
	}
	draft, err := box.CreateDraft(DraftInput{Grade: "poly-A", Spec: validSpec(), ExpiresAt: now + 100, Reason: "r", CreatedBy: "ops"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := box.RunReplay(draft.ID, fakeBatches{[]EvidenceBatch{twoProofBatch("poly-A")}}, now+10); err != nil {
		t.Fatal(err)
	}
	if _, err := box.Approve(draft.ID, "lead", "ok", now+20); err != nil {
		t.Fatal(err)
	}
	if got := box.Book().Current("poly-A").Number; got != BuiltinNumber {
		t.Fatalf("approved-but-unpublished draft must not change live rule, got %d", got)
	}
	if got := box.Book().Current("poly-B").Number; got != BuiltinNumber {
		t.Fatalf("other grade must stay on builtin, got %d", got)
	}
}

func TestPublishBumpsVersionClosesIntervalAndIsIdempotent(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "rules.json"))
	box := NewBox(store)
	const now = 1000
	draft, err := box.CreateDraft(DraftInput{Grade: "poly-A", Spec: validSpec(), ExpiresAt: now + 100, Reason: "r", CreatedBy: "ops"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := box.RunReplay(draft.ID, fakeBatches{[]EvidenceBatch{twoProofBatch("poly-A")}}, now+10); err != nil {
		t.Fatal(err)
	}
	if _, err := box.Approve(draft.ID, "lead", "ok", now+20); err != nil {
		t.Fatal(err)
	}
	version, already, err := box.Publish(draft.ID, now+30)
	if err != nil || already || version.Number != 2 {
		t.Fatalf("unexpected publish: %+v already=%v err=%v", version, already, err)
	}
	if got := box.Book().Current("poly-A").Number; got != 2 {
		t.Fatalf("live rule must be v2, got %d", got)
	}
	if got := box.Book().VersionAt("poly-A", now+5).Number; got != BuiltinNumber {
		t.Fatalf("historical instant must resolve to builtin, got %d", got)
	}
	if got := box.Book().VersionAt("poly-A", now+30).Number; got != 2 {
		t.Fatalf("current instant must resolve to v2, got %d", got)
	}
	again, already, err := box.Publish(draft.ID, now+40)
	if err != nil || !already || again.Number != 2 {
		t.Fatalf("republish must be idempotent: %+v already=%v err=%v", again, already, err)
	}
}

func TestPublishChainsClosePriorInterval(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "rules.json"))
	box := NewBox(store)
	createApprovePublish := func(minimum, now int64) {
		spec := RuleSpec{MinimumEvidence: int(minimum), RequireIndependent: true, AllowedPhases: []model.Phase{model.OnSpec}}
		draft, err := box.CreateDraft(DraftInput{Grade: "poly-A", Spec: spec, ExpiresAt: now + 100, Reason: "r", CreatedBy: "ops"}, now)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := box.RunReplay(draft.ID, fakeBatches{[]EvidenceBatch{twoProofBatch("poly-A")}}, now+10); err != nil {
			t.Fatal(err)
		}
		if _, err := box.Approve(draft.ID, "lead", "ok", now+20); err != nil {
			t.Fatal(err)
		}
		if _, _, err := box.Publish(draft.ID, now+30); err != nil {
			t.Fatal(err)
		}
	}
	createApprovePublish(3, 1000)
	createApprovePublish(1, 2000)
	if got := box.Book().VersionAt("poly-A", 1500).Number; got != 2 {
		t.Fatalf("interval between publishes must be v2, got %d", got)
	}
	if got := box.Book().VersionAt("poly-A", 2500).Number; got != 3 {
		t.Fatalf("after second publish must be v3, got %d", got)
	}
	versions := box.Book().Versions("poly-A")
	if len(versions) != 3 || versions[1].EffectiveTo != 2030 {
		t.Fatalf("prior version must be closed at successor publication: %+v", versions)
	}
}

func TestRejectDraft(t *testing.T) {
	box := NewBox(NewStore(filepath.Join(t.TempDir(), "rules.json")))
	const now = 1000
	draft, err := box.CreateDraft(DraftInput{Grade: "poly-A", Spec: validSpec(), ExpiresAt: now + 100, Reason: "r", CreatedBy: "ops"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := box.Reject(draft.ID, "lead", "no", now+200); err != nil {
		t.Fatalf("reject must be allowed past expiry: %v", err)
	}
	got, ok := box.Draft(draft.ID)
	if !ok || got.Status != StatusRejected {
		t.Fatalf("draft must be rejected: %+v", got)
	}
	if _, err := box.Approve(draft.ID, "lead", "ok", now); err == nil {
		t.Fatal("rejected draft must not be approvable")
	}
	if got := box.Book().Current("poly-A").Number; got != BuiltinNumber {
		t.Fatalf("reject must not change live rule, got %d", got)
	}
}

func TestStoreReloadRehydratesState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rules.json")
	box := NewBox(NewStore(path))
	const now = 1000
	draft, err := box.CreateDraft(DraftInput{Grade: "poly-A", Spec: validSpec(), ExpiresAt: now + 100, Reason: "r", CreatedBy: "ops"}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := box.RunReplay(draft.ID, fakeBatches{[]EvidenceBatch{twoProofBatch("poly-A")}}, now+10); err != nil {
		t.Fatal(err)
	}
	if _, err := box.Approve(draft.ID, "lead", "ok", now+20); err != nil {
		t.Fatal(err)
	}
	if _, _, err := box.Publish(draft.ID, now+30); err != nil {
		t.Fatal(err)
	}

	reloaded := NewBox(NewStore(path))
	if err := reloaded.Load(); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := reloaded.Book().Current("poly-A").Number; got != 2 {
		t.Fatalf("reloaded live rule must be v2, got %d", got)
	}
	got, ok := reloaded.Draft(draft.ID)
	if !ok || got.Status != StatusPublished || got.PublishedVersion != 2 {
		t.Fatalf("reloaded draft mismatch: %+v", got)
	}
}

func TestLoadMissingFileSeedsBuiltin(t *testing.T) {
	box := NewBox(NewStore(filepath.Join(t.TempDir(), "absent.json")))
	if err := box.Load(); err != nil {
		t.Fatalf("missing file must not error: %v", err)
	}
	if got := box.Book().Current("any-grade").Number; got != BuiltinNumber {
		t.Fatalf("missing file must seed builtin, got %d", got)
	}
}
