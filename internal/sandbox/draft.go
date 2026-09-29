package sandbox

import (
	"errors"
	"github.com/google/uuid"
	"github.com/wyw14/solo-039/internal/interlock"
	"github.com/wyw14/solo-039/internal/model"
)

// Status is the lifecycle state of a sandbox draft.
type Status string

const (
	StatusDraft     Status = "draft"
	StatusApproved  Status = "approved"
	StatusRejected  Status = "rejected"
	StatusPublished Status = "published"
)

// RuleSpec is the operator-authored candidate criterion. Phases use the model's
// phase vocabulary so drafts cannot invent stages.
type RuleSpec struct {
	MinimumEvidence    int
	RequireIndependent bool
	AllowedPhases      []model.Phase
	RequiredSources    []string
}

func (s RuleSpec) Validate() error {
	if s.MinimumEvidence < 1 {
		return errors.New("minimum evidence must be at least one")
	}
	if len(s.AllowedPhases) == 0 {
		return errors.New("at least one allowed phase is required")
	}
	knownPhases := make(map[model.Phase]struct{}, len(model.Phases()))
	for _, phase := range model.Phases() {
		knownPhases[phase] = struct{}{}
	}
	seenPhases := make(map[model.Phase]struct{}, len(s.AllowedPhases))
	for _, phase := range s.AllowedPhases {
		if _, ok := knownPhases[phase]; !ok {
			return errors.New("unknown allowed phase: " + string(phase))
		}
		if _, ok := seenPhases[phase]; ok {
			return errors.New("duplicate allowed phase: " + string(phase))
		}
		seenPhases[phase] = struct{}{}
	}
	seenSources := make(map[string]struct{}, len(s.RequiredSources))
	for _, source := range s.RequiredSources {
		if source == "" {
			return errors.New("required source must not be empty")
		}
		if _, ok := seenSources[source]; ok {
			return errors.New("duplicate required source: " + source)
		}
		seenSources[source] = struct{}{}
	}
	return nil
}

func (s RuleSpec) Rule() interlock.Rule {
	return interlock.Rule{
		MinimumEvidence:    s.MinimumEvidence,
		RequireIndependent: s.RequireIndependent,
		AllowedPhases:      append([]model.Phase(nil), s.AllowedPhases...),
		RequiredSources:    append([]string(nil), s.RequiredSources...),
	}
}

// DraftInput is the creation request. ExpiresAt is a unix-seconds review
// deadline: it bounds draft -> approved, never the published version.
type DraftInput struct {
	Grade     string
	Spec      RuleSpec
	ExpiresAt int64
	Reason    string
	CreatedBy string
}

func (in DraftInput) Validate(now int64) error {
	if err := model.ValidateGrade(in.Grade); err != nil {
		return err
	}
	if err := in.Spec.Validate(); err != nil {
		return err
	}
	if in.ExpiresAt <= now {
		return errors.New("expires_at must be in the future")
	}
	if in.Reason == "" {
		return errors.New("reason is required")
	}
	if in.CreatedBy == "" {
		return errors.New("created_by is required")
	}
	return nil
}

// Draft is the persisted sandbox proposal. Replay is attached when a replay has
// been run and snapshotted at replay time (BasedOn records the baseline).
type Draft struct {
	ID               uuid.UUID
	Grade            string
	Candidate        interlock.Rule
	ExpiresAt        int64
	Reason           string
	CreatedBy        string
	Status           Status
	CreatedAt        int64
	BasedOn          int
	Replay           *ReplayReport
	ApprovedBy       string
	ApprovedAt       int64
	ApprovalComment  string
	RejectedBy       string
	RejectedAt       int64
	RejectComment    string
	PublishedVersion int
}

func (d Draft) Expired(now int64) bool { return now >= d.ExpiresAt }
