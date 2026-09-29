package interlock

import (
	"errors"
	"github.com/wyw14/solo-039/internal/model"
)

type Permit struct {
	Allowed     bool
	Reasons     []string
	Evidence    []model.Proof
	RuleVersion int
}

func (p Permit) Error() error {
	if p.Allowed {
		return nil
	}
	if len(p.Reasons) == 0 {
		return errors.New("permit denied")
	}
	return errors.New(p.Reasons[0])
}

func Independent(proofs []model.Proof) bool {
	seen := make(map[string]struct{})
	for _, proof := range proofs {
		if !proof.Independent {
			continue
		}
		if _, ok := seen[proof.Source]; ok {
			return false
		}
		seen[proof.Source] = struct{}{}
	}
	return len(seen) == len(proofs)
}
