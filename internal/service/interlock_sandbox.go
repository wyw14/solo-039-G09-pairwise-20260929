package service

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/wyw14/solo-039/internal/interlock"
	"github.com/wyw14/solo-039/internal/model"
)

// RuleDraftRequest 是规则草案的录入参数。
type RuleDraftRequest struct {
	Grade              string   `json:"grade"`
	MinimumEvidence    int      `json:"minimum_evidence"`
	RequireIndependent bool     `json:"require_independent"`
	RequiredSources    []string `json:"required_sources"`
	AllowedPhases      []string `json:"allowed_phases"`
	EffectiveAt        int64    `json:"effective_at"` // unix 秒，0 表示批准即生效
	ExpiresAt          int64    `json:"expires_at"`   // unix 秒，0 表示默认 24 小时后失效
	Rationale          string   `json:"rationale"`
	CreatedBy          string   `json:"created_by"`
}

// RuleReviewRequest 是审批操作参数。
type RuleReviewRequest struct {
	Approver string `json:"approver"`
	Reason   string `json:"reason"`
}

var knownPhases = map[model.Phase]bool{
	model.Preparing: true, model.Warming: true, model.Stabilizing: true,
	model.OnSpec: true, model.Transitioning: true, model.Shutting: true, model.Stopped: true,
}

func (p *Plant) SubmitRuleDraft(req RuleDraftRequest) (interlock.Draft, error) {
	phases, err := parsePhases(req.AllowedPhases)
	if err != nil {
		return interlock.Draft{}, err
	}
	now := time.Now()
	expiresAt := req.ExpiresAt
	if expiresAt == 0 {
		expiresAt = now.Add(24 * time.Hour).Unix()
	}
	return p.RuleDrafts.Submit(interlock.DraftInput{
		Grade: req.Grade,
		Criteria: interlock.RuleCriteria{
			MinimumEvidence:    req.MinimumEvidence,
			RequireIndependent: req.RequireIndependent,
			RequiredSources:    req.RequiredSources,
			AllowedPhases:      phases,
			Grade:              req.Grade,
		},
		EffectiveAt: req.EffectiveAt,
		ExpiresAt:   expiresAt,
		Rationale:   req.Rationale,
		CreatedBy:   req.CreatedBy,
	}, now)
}

func parsePhases(values []string) ([]model.Phase, error) {
	phases := make([]model.Phase, 0, len(values))
	for _, value := range values {
		phase := model.Phase(value)
		if !knownPhases[phase] {
			return nil, errors.New("unknown phase: " + value)
		}
		phases = append(phases, phase)
	}
	return phases, nil
}

// ReplayRuleDraft 用已有批次留痕的证据回放草案。回放批次取自决定日志：
// 每个产品修订版最近一次路由判定构成一个批次快照。
func (p *Plant) ReplayRuleDraft(draftID uuid.UUID) (interlock.ReplaySummary, error) {
	batches := p.replayBatches()
	return p.RuleDrafts.Replay(draftID, batches, time.Now())
}

func (p *Plant) replayBatches() []interlock.BatchEvidence {
	byRevision := make(map[string]interlock.BatchEvidence)
	for _, decision := range p.DecisionLog.List() {
		if decision.Context != "route-product" {
			continue
		}
		// 同一修订版可能有多次判定（先拒后允），保留证据最全的一次。
		existing := byRevision[decision.Revision.String()]
		if len(decision.Proofs) >= len(existing.Proofs) {
			byRevision[decision.Revision.String()] = interlock.BatchEvidence{
				BatchID:   decision.Revision.String(),
				Grade:     decision.Grade,
				Phase:     decision.Phase,
				Proofs:    append([]model.Proof(nil), decision.Proofs...),
				DecidedAt: decision.At,
			}
		}
	}
	batches := make([]interlock.BatchEvidence, 0, len(byRevision))
	for _, batch := range byRevision {
		batches = append(batches, batch)
	}
	return batches
}

func (p *Plant) ApproveRuleDraft(draftID uuid.UUID, approver string) (interlock.Draft, error) {
	return p.RuleDrafts.Approve(draftID, approver, time.Now())
}

func (p *Plant) RejectRuleDraft(draftID uuid.UUID, approver, reason string) (interlock.Draft, error) {
	return p.RuleDrafts.Reject(draftID, approver, reason, time.Now())
}

func (p *Plant) ListRuleDrafts() []interlock.Draft {
	p.RuleDrafts.Sweep(time.Now())
	return p.RuleDrafts.List()
}

func (p *Plant) GetRuleDraft(id uuid.UUID) (interlock.Draft, error) {
	return p.RuleDrafts.Get(id)
}

func (p *Plant) RuleVersions() []interlock.Rule { return p.RuleBook.Versions() }

func (p *Plant) InterlockDecisions() []interlock.Decision { return p.DecisionLog.List() }

// ExplainInterlockDecision 按决定发生时的规则版本重新解释历史决定。
func (p *Plant) ExplainInterlockDecision(id uuid.UUID) (interlock.Explanation, error) {
	decision, ok := p.DecisionLog.Get(id)
	if !ok {
		return interlock.Explanation{}, errors.New("interlock decision not found")
	}
	return interlock.HistoricalExplanation(p.RuleBook, p.RuleEngine, decision)
}
