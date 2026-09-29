package interlock

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/wyw14/solo-039/internal/model"
)

// Decision 是一次在线许可判定的留痕。它快照了当时使用的规则版本与全部证据，
// 因此即使规则簿后来发布了新版本，仍能用当时的版本重新解释该决定。
type Decision struct {
	ID          uuid.UUID
	Context     string // 判定场景，如 route-product、heat、recovery
	CampaignID  uuid.UUID
	Revision    uuid.UUID
	Grade       string
	Phase       model.Phase
	Proofs      []model.Proof
	Allowed     bool
	Reasons     []string
	RuleVersion int
	At          int64
}

type DecisionLog struct {
	mu        sync.Mutex
	decisions []Decision
}

func NewDecisionLog() *DecisionLog {
	return &DecisionLog{decisions: make([]Decision, 0, 64)}
}

// Record 追加一条决定记录。证据以快照形式保存，后续证据目录变化不影响历史。
func (l *DecisionLog) Record(d Decision) Decision {
	if d.ID == uuid.Nil {
		d.ID = uuid.New()
	}
	if d.At == 0 {
		d.At = time.Now().Unix()
	}
	d.Proofs = append([]model.Proof(nil), d.Proofs...)
	d.Reasons = append([]string(nil), d.Reasons...)
	l.mu.Lock()
	l.decisions = append(l.decisions, d)
	l.mu.Unlock()
	return d
}

func (l *DecisionLog) Latest() (Decision, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.decisions) == 0 {
		return Decision{}, false
	}
	return l.decisions[len(l.decisions)-1], true
}

func (l *DecisionLog) List() []Decision {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]Decision, len(l.decisions))
	copy(out, l.decisions)
	return out
}

func (l *DecisionLog) ForRevision(revision string) []Decision {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]Decision, 0)
	for _, decision := range l.decisions {
		if decision.Revision.String() == revision {
			out = append(out, decision)
		}
	}
	return out
}

func (l *DecisionLog) Get(id uuid.UUID) (Decision, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, decision := range l.decisions {
		if decision.ID == id {
			return decision, true
		}
	}
	return Decision{}, false
}

func (l *DecisionLog) Count() int { l.mu.Lock(); defer l.mu.Unlock(); return len(l.decisions) }

// HistoricalExplanation 用决定记录中保存的规则版本重新评估快照证据。
// 取到的是历史规则而非当前规则，保证旧决定永远按当时判据解释。
func HistoricalExplanation(book *RuleBook, engine *RuleEngine, d Decision) (Explanation, error) {
	rule, ok := book.Version(d.RuleVersion)
	if !ok {
		return Explanation{}, fmt.Errorf("rule version %d no longer available", d.RuleVersion)
	}
	permit := engine.Evaluate(rule, DecisionInput{
		Proofs:   d.Proofs,
		Campaign: model.Campaign{ID: d.CampaignID, Revision: d.Revision, Grade: d.Grade, Phase: d.Phase},
		At:       time.Unix(d.At, 0),
	})
	explanation := Explain(permit)
	return explanation, nil
}

// OnlineJudge 是在线判定入口：选取当前生效规则、执行评估并留痕。
// 它只读取规则簿中已发布的版本，草案对其完全不可见。
type OnlineJudge struct {
	Book   *RuleBook
	Engine *RuleEngine
	Log    *DecisionLog
}

func NewOnlineJudge(book *RuleBook, engine *RuleEngine, log *DecisionLog) *OnlineJudge {
	return &OnlineJudge{Book: book, Engine: engine, Log: log}
}

// Decide 按 grade 在 at 时刻的在线规则给出许可。没有任何生效规则时拒绝放行，
// 拒绝同样留痕，便于事后解释为何该时刻无法取得许可。
func (j *OnlineJudge) Decide(context string, in DecisionInput) (Permit, Decision, error) {
	if in.At.IsZero() {
		in.At = time.Now()
	}
	rule, ok := j.Book.ActiveAt(in.Campaign.Grade, in.At)
	permit := Permit{Evidence: append([]model.Proof(nil), in.Proofs...)}
	if !ok {
		permit.Reasons = []string{"no published interlock rule is active"}
	} else {
		permit = j.Engine.Evaluate(rule, in)
	}
	decision := j.Log.Record(Decision{
		Context:     context,
		CampaignID:  in.Campaign.ID,
		Revision:    in.Campaign.Revision,
		Grade:       in.Campaign.Grade,
		Phase:       in.Campaign.Phase,
		Proofs:      in.Proofs,
		Allowed:     permit.Allowed,
		Reasons:     permit.Reasons,
		RuleVersion: permit.RuleVersion,
		At:          in.At.Unix(),
	})
	if !ok {
		return permit, decision, errors.New(permit.Reasons[0])
	}
	if !permit.Allowed {
		return permit, decision, permit.Error()
	}
	return permit, decision, nil
}
