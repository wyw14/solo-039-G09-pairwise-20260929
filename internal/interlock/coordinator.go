package interlock

import (
	"time"

	"github.com/wyw14/solo-039/internal/model"
)

type Coordinator struct {
	Minimum int
	book    *RuleBook
	engine  *RuleEngine
}

func NewCoordinator() *Coordinator { return &Coordinator{Minimum: 2} }

// BindRuleBook 让既有协调器改用规则簿中当前生效的版本判定。
// 未绑定时保留出厂基线行为，便于恢复路径与测试独立使用。
func (c *Coordinator) BindRuleBook(book *RuleBook) {
	c.book = book
	c.engine = NewRuleEngine()
}

func (c *Coordinator) Evaluate(proofs []model.Proof, campaign model.Campaign) Permit {
	if c.book != nil {
		if rule, ok := c.book.ActiveAt(campaign.Grade, time.Now()); ok {
			return c.engine.Evaluate(rule, DecisionInput{Proofs: proofs, Campaign: campaign, At: time.Now()})
		}
	}
	return c.evaluateBaseline(proofs, campaign)
}

// evaluateBaseline 保持规则簿出现之前的出厂判据：证据数量不少于 Minimum、
// 来源相互独立、批次处于 on-spec 阶段。
func (c *Coordinator) evaluateBaseline(proofs []model.Proof, campaign model.Campaign) Permit {
	p := Permit{Evidence: append([]model.Proof(nil), proofs...)}
	minimum := c.Minimum
	if minimum <= 0 {
		minimum = 2
	}
	if len(proofs) < minimum {
		p.Reasons = append(p.Reasons, "insufficient evidence")
		return p
	}
	if !Independent(proofs) {
		p.Reasons = append(p.Reasons, "evidence is not independent")
		return p
	}
	if campaign.Phase != model.OnSpec {
		p.Reasons = append(p.Reasons, "campaign is not on-spec")
		return p
	}
	p.Allowed = true
	return p
}
