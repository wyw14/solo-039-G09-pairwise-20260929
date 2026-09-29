package interlock

import "github.com/wyw14/solo-039/internal/model"

type Coordinator struct{ Rule Rule }

func NewCoordinator() *Coordinator { return &Coordinator{Rule: DefaultRule()} }

func (c *Coordinator) Evaluate(proofs []model.Proof, campaign model.Campaign) Permit {
	return c.Rule.Evaluate(proofs, campaign)
}
