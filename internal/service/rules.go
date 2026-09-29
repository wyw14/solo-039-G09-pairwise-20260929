package service

import (
	"github.com/google/uuid"
	"github.com/wyw14/solo-039/internal/sandbox"
	"time"
)

// Batches adapts campaign history plus the evidence catalog into sandbox
// batches: one entry per distinct campaign revision that has accumulated at
// least one independent proof. It makes the Plant a sandbox.BatchSource.
func (p *Plant) Batches() []sandbox.EvidenceBatch {
	seen := make(map[string]struct{})
	batches := make([]sandbox.EvidenceBatch, 0)
	for _, change := range p.CampaignHistory.Changes() {
		revision := change.After.Revision.String()
		if _, ok := seen[revision]; ok {
			continue
		}
		seen[revision] = struct{}{}
		proofs := p.Evidence.Independent(revision)
		if len(proofs) == 0 {
			continue
		}
		batches = append(batches, sandbox.EvidenceBatch{Revision: revision, Grade: change.After.Grade, Proofs: proofs})
	}
	return batches
}

func (p *Plant) CreateRuleDraft(in sandbox.DraftInput) (sandbox.Draft, error) {
	return p.Rules.CreateDraft(in, time.Now().Unix())
}

func (p *Plant) ReplayRuleDraft(id uuid.UUID) (sandbox.ReplayReport, error) {
	return p.Rules.RunReplay(id, p, time.Now().Unix())
}

func (p *Plant) ApproveRuleDraft(id uuid.UUID, by, comment string) (sandbox.Draft, error) {
	return p.Rules.Approve(id, by, comment, time.Now().Unix())
}

func (p *Plant) RejectRuleDraft(id uuid.UUID, by, comment string) error {
	return p.Rules.Reject(id, by, comment, time.Now().Unix())
}

func (p *Plant) PublishRuleDraft(id uuid.UUID) (sandbox.RuleVersion, bool, error) {
	return p.Rules.Publish(id, time.Now().Unix())
}

func (p *Plant) RuleDrafts() []sandbox.Draft { return p.Rules.Drafts() }

func (p *Plant) NowUnix() int64 { return time.Now().Unix() }

func (p *Plant) RuleDraft(id uuid.UUID) (sandbox.Draft, bool) { return p.Rules.Draft(id) }

func (p *Plant) RuleVersions(grade string) []sandbox.RuleVersion {
	return p.Rules.Book().Versions(grade)
}

func (p *Plant) FindRuleVersion(number int, grade string) (sandbox.RuleVersion, bool) {
	return p.Rules.Book().Find(number, grade)
}

// ActiveRule resolves the version in force for a grade at the given instant;
// at <= 0 means the current time.
func (p *Plant) ActiveRule(grade string, at int64) sandbox.RuleVersion {
	if at <= 0 {
		at = time.Now().Unix()
	}
	return p.Rules.Book().VersionAt(grade, at)
}
