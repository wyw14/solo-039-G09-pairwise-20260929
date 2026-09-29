package interlock

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/wyw14/solo-039/internal/model"
)

// DraftStatus 是规则草案的审批状态。
type DraftStatus string

const (
	DraftPending   DraftStatus = "pending"   // 已录入，等待审核
	DraftApproved  DraftStatus = "approved"  // 已批准（发布前的瞬时状态）
	DraftPublished DraftStatus = "published" // 已发布为规则簿新版本
	DraftRejected  DraftStatus = "rejected"  // 审核驳回
	DraftExpired   DraftStatus = "expired"   // 超过失效时间未审核
)

// Draft 是规则变更沙盒中的草案。它只是候选判据的副本：
// 审核通过并发布之前，不会以任何形式进入在线规则簿。
type Draft struct {
	ID          uuid.UUID
	Grade       string // 目标品级，为空表示全局规则
	Criteria    RuleCriteria
	EffectiveAt int64 // 批准后新版本的计划生效时间（unix 秒），0 表示批准即生效
	ExpiresAt   int64 // 草案失效时间，逾期未审核自动作废
	Rationale   string
	CreatedBy   string
	CreatedAt   int64

	Status           DraftStatus
	ReviewedBy       string
	ReviewedAt       int64
	RejectReason     string
	PublishedVersion int
	LastReplay       *ReplaySummary
}

// BatchEvidence 是一个已有批次在历史判定时刻的证据快照，用于沙盒回放。
type BatchEvidence struct {
	BatchID   string
	Grade     string
	Phase     model.Phase
	Proofs    []model.Proof
	DecidedAt int64 // unix 秒，选取该时刻在线规则作为对照
}

// BatchReplay 是单个批次在新旧规则下的许可对比。
type BatchReplay struct {
	BatchID    string
	OldVersion int
	OldAllowed bool
	OldReasons []string
	NewAllowed bool
	NewReasons []string
	Changed    bool
	// Tightened 表示旧规则放行、新规则拒绝（判据收紧）。
	Tightened bool
	// Loosened 表示旧规则拒绝、新规则放行（判据放宽）。
	Loosened bool
}

// ReplaySummary 是草案对一批历史证据的回放结果。
type ReplaySummary struct {
	DraftID    uuid.UUID
	RanAt      int64
	Batches    int
	Changed    int
	Tightened  int
	Loosened   int
	Results    []BatchReplay
	OldVersion int // 回放在没有品级专用旧规则时记录全局基线版本，0 表示无在线规则
}

// DraftInput 是录入草案的请求。
type DraftInput struct {
	Grade       string
	Criteria    RuleCriteria
	EffectiveAt int64
	ExpiresAt   int64
	Rationale   string
	CreatedBy   string
}

// DraftStore 保存全部草案并驱动 录入→回放→审核→发布 流程。
// 它持有规则簿的引用，但只在批准路径上调用 Publish，
// pending/rejected/expired 状态的草案对在线判定不可见。
type DraftStore struct {
	mu     sync.Mutex
	drafts map[uuid.UUID]Draft
	book   *RuleBook
	engine *RuleEngine
}

func NewDraftStore(book *RuleBook, engine *RuleEngine) *DraftStore {
	return &DraftStore{drafts: make(map[uuid.UUID]Draft), book: book, engine: engine}
}

// Submit 录入草案。失效时间必须晚于当前时间，草案创建后停留 pending。
func (s *DraftStore) Submit(in DraftInput, now time.Time) (Draft, error) {
	if err := ValidateCriteria(in.Criteria); err != nil {
		return Draft{}, err
	}
	if in.ExpiresAt <= now.Unix() {
		return Draft{}, errors.New("draft expiry must be in the future")
	}
	if in.EffectiveAt != 0 && in.EffectiveAt < now.Unix() {
		return Draft{}, errors.New("effective time cannot be before submission")
	}
	draft := Draft{
		ID:          uuid.New(),
		Grade:       in.Grade,
		Criteria:    in.Criteria,
		EffectiveAt: in.EffectiveAt,
		ExpiresAt:   in.ExpiresAt,
		Rationale:   in.Rationale,
		CreatedBy:   in.CreatedBy,
		CreatedAt:   now.Unix(),
		Status:      DraftPending,
	}
	s.mu.Lock()
	s.drafts[draft.ID] = draft
	s.mu.Unlock()
	return draft, nil
}

func (s *DraftStore) Get(id uuid.UUID) (Draft, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	draft, ok := s.drafts[id]
	if !ok {
		return Draft{}, fmt.Errorf("draft %s not found", id)
	}
	return draft, nil
}

func (s *DraftStore) List() []Draft {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Draft, len(s.drafts))
	i := 0
	for _, draft := range s.drafts {
		out[i] = draft
		i++
	}
	return out
}

// Replay 用候选规则与各批次当时在线的规则分别判定，输出差异。
// 回放只读规则簿、不修改草案状态（仅缓存最近一次结果），可重复执行。
func (s *DraftStore) Replay(id uuid.UUID, batches []BatchEvidence, now time.Time) (ReplaySummary, error) {
	s.mu.Lock()
	draft, ok := s.drafts[id]
	s.mu.Unlock()
	if !ok {
		return ReplaySummary{}, fmt.Errorf("draft %s not found", id)
	}
	if draft.Status == DraftPublished {
		return ReplaySummary{}, errors.New("draft already published")
	}
	if now.Unix() > draft.ExpiresAt {
		s.markExpired(id)
		return ReplaySummary{}, errors.New("draft has expired")
	}

	candidate := Rule{Version: -1, Grade: draft.Grade, Criteria: draft.Criteria}
	summary := ReplaySummary{DraftID: id, RanAt: now.Unix(), Results: make([]BatchReplay, 0, len(batches))}
	for _, batch := range batches {
		oldRule, oldOK := s.book.ActiveAt(batch.Grade, time.Unix(batch.DecidedAt, 0))
		if !ok {
			// 批次品级没有专用规则时用全局规则对照；仍没有则按“无规则即拒绝”回放。
			oldRule, oldOK = s.book.ActiveAt("", time.Unix(batch.DecidedAt, 0))
		}
		oldPermit := Permit{}
		if oldOK {
			oldPermit = s.engine.Evaluate(oldRule, DecisionInput{
				Proofs:   batch.Proofs,
				Campaign: model.Campaign{Grade: batch.Grade, Phase: batch.Phase},
			})
		} else {
			oldPermit.Reasons = []string{"no active rule at decision time"}
		}
		newPermit := s.engine.Evaluate(candidate, DecisionInput{
			Proofs:   batch.Proofs,
			Campaign: model.Campaign{Grade: batch.Grade, Phase: batch.Phase},
		})
		replay := BatchReplay{
			BatchID:    batch.BatchID,
			OldVersion: oldPermit.RuleVersion,
			OldAllowed: oldPermit.Allowed,
			OldReasons: append([]string(nil), oldPermit.Reasons...),
			NewAllowed: newPermit.Allowed,
			NewReasons: append([]string(nil), newPermit.Reasons...),
			Changed:    oldPermit.Allowed != newPermit.Allowed,
		}
		if replay.Changed {
			summary.Changed++
			if oldPermit.Allowed && !newPermit.Allowed {
				replay.Tightened = true
				summary.Tightened++
			} else {
				replay.Loosened = true
				summary.Loosened++
			}
		}
		summary.Results = append(summary.Results, replay)
		if oldOK && summary.OldVersion == 0 {
			summary.OldVersion = oldRule.Version
		}
	}
	summary.Batches = len(summary.Results)

	s.mu.Lock()
	if current, ok := s.drafts[id]; ok && current.Status == DraftPending {
		current.LastReplay = &summary
		s.drafts[id] = current
	}
	s.mu.Unlock()
	return summary, nil
}

// Approve 审核通过草案并把候选判据作为新版本发布到规则簿。
// 这是草案触达在线规则的唯一路径；过期或非 pending 草案一律拒绝。
func (s *DraftStore) Approve(id uuid.UUID, approver string, now time.Time) (Draft, error) {
	if approver == "" {
		return Draft{}, errors.New("approver is required")
	}
	s.mu.Lock()
	draft, ok := s.drafts[id]
	s.mu.Unlock()
	if !ok {
		return Draft{}, fmt.Errorf("draft %s not found", id)
	}
	if draft.Status != DraftPending {
		return Draft{}, fmt.Errorf("draft is %s, only pending drafts can be approved", draft.Status)
	}
	if now.Unix() > draft.ExpiresAt {
		s.markExpired(id)
		return Draft{}, errors.New("draft has expired before approval")
	}

	effectiveAt := draft.EffectiveAt
	if effectiveAt == 0 {
		effectiveAt = now.Unix()
	}
	candidate := Rule{
		Grade:       draft.Grade,
		Criteria:    draft.Criteria,
		EffectiveAt: effectiveAt,
		ApprovedBy:  approver,
	}
	published, err := s.book.Publish(candidate, now)
	if err != nil {
		return Draft{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	// 发布期间状态可能被并发改动，发布前再次确认。
	current, ok := s.drafts[id]
	if !ok || current.Status != DraftPending {
		return Draft{}, errors.New("draft changed concurrently during publish")
	}
	current.Status = DraftPublished
	current.ReviewedBy = approver
	current.ReviewedAt = now.Unix()
	current.PublishedVersion = published.Version
	s.drafts[id] = current
	return current, nil
}

// Reject 审核驳回，草案不再可发布，对在线规则没有任何影响。
func (s *DraftStore) Reject(id uuid.UUID, approver, reason string, now time.Time) (Draft, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	draft, ok := s.drafts[id]
	if !ok {
		return Draft{}, fmt.Errorf("draft %s not found", id)
	}
	if draft.Status != DraftPending {
		return Draft{}, fmt.Errorf("draft is %s, only pending drafts can be rejected", draft.Status)
	}
	draft.Status = DraftRejected
	draft.ReviewedBy = approver
	draft.ReviewedAt = now.Unix()
	draft.RejectReason = reason
	s.drafts[id] = draft
	return draft, nil
}

// Sweep 把超过失效时间仍未审核的草案标记为 expired。
func (s *DraftStore) Sweep(now time.Time) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for id, draft := range s.drafts {
		if draft.Status == DraftPending && now.Unix() > draft.ExpiresAt {
			draft.Status = DraftExpired
			s.drafts[id] = draft
			count++
		}
	}
	return count
}

func (s *DraftStore) markExpired(id uuid.UUID) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if draft, ok := s.drafts[id]; ok && draft.Status == DraftPending {
		draft.Status = DraftExpired
		s.drafts[id] = draft
	}
}
