package sandbox

import (
	"errors"
	"github.com/google/uuid"
	"sync"
)

// Box is the sandbox orchestrator: it owns the rule book and the draft
// lifecycle and persists every mutation. Unapproved drafts never reach the
// book, so online evaluation is unaffected until publish.
type Box struct {
	mu     sync.Mutex
	book   *RuleBook
	drafts []Draft
	store  *Store
}

func NewBox(store *Store) *Box {
	return &Box{book: NewRuleBook(), drafts: make([]Draft, 0), store: store}
}

// Load restores the persisted state. A missing file (or empty version list)
// seeds the built-in baseline.
func (b *Box) Load() error {
	snapshot, err := b.store.Load()
	if err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.book.replace(snapshot.Versions)
	b.drafts = snapshot.Drafts
	if b.drafts == nil {
		b.drafts = make([]Draft, 0)
	}
	return nil
}

func (b *Box) Book() *RuleBook { return b.book }

func (b *Box) Drafts() []Draft {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]Draft(nil), b.drafts...)
}

func (b *Box) Draft(id uuid.UUID) (Draft, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, draft := range b.drafts {
		if draft.ID == id {
			return draft, true
		}
	}
	return Draft{}, false
}

func (b *Box) CreateDraft(in DraftInput, now int64) (Draft, error) {
	if err := in.Validate(now); err != nil {
		return Draft{}, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	draft := Draft{
		ID:        uuid.New(),
		Grade:     in.Grade,
		Candidate: in.Spec.Rule(),
		ExpiresAt: in.ExpiresAt,
		Reason:    in.Reason,
		CreatedBy: in.CreatedBy,
		Status:    StatusDraft,
		CreatedAt: now,
		BasedOn:   b.book.Current(in.Grade).Number,
	}
	drafts := append(append([]Draft(nil), b.drafts...), draft)
	if err := b.persist(drafts); err != nil {
		return Draft{}, err
	}
	b.drafts = drafts
	return draft, nil
}

// RunReplay replays current evidence batches against the grade's in-force
// baseline and the draft candidate, and snapshots the report onto the draft.
func (b *Box) RunReplay(id uuid.UUID, src BatchSource, now int64) (ReplayReport, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	idx := -1
	for i := range b.drafts {
		if b.drafts[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return ReplayReport{}, errors.New("draft not found")
	}
	draft := b.drafts[idx]
	if draft.Status != StatusDraft {
		return ReplayReport{}, errors.New("draft is not open")
	}
	base := b.book.Current(draft.Grade)
	report := RunReplay(base.Rule, draft.Candidate, src.Batches(), now)
	report.BaseVersion = base.Number
	draft.Replay = &report
	draft.BasedOn = base.Number
	drafts := append([]Draft(nil), b.drafts...)
	drafts[idx] = draft
	if err := b.persist(drafts); err != nil {
		return ReplayReport{}, err
	}
	b.drafts = drafts
	return report, nil
}

func (b *Box) Approve(id uuid.UUID, by, comment string, now int64) (Draft, error) {
	if by == "" {
		return Draft{}, errors.New("approver is required")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	idx, err := b.openDraftIndex(id)
	if err != nil {
		return Draft{}, err
	}
	draft := b.drafts[idx]
	if draft.Expired(now) {
		return Draft{}, errors.New("draft has expired")
	}
	if draft.Replay == nil {
		return Draft{}, errors.New("replay is required before approval")
	}
	if draft.Replay.Batches < 1 {
		return Draft{}, errors.New("replay has no evidence batches")
	}
	draft.Status = StatusApproved
	draft.ApprovedBy = by
	draft.ApprovedAt = now
	draft.ApprovalComment = comment
	drafts := append([]Draft(nil), b.drafts...)
	drafts[idx] = draft
	if err := b.persist(drafts); err != nil {
		return Draft{}, err
	}
	b.drafts = drafts
	return draft, nil
}

// Reject closes a draft without publishing. Rejection stays available past
// the review deadline.
func (b *Box) Reject(id uuid.UUID, by, comment string, now int64) error {
	if by == "" {
		return errors.New("reviewer is required")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	idx, err := b.openDraftIndex(id)
	if err != nil {
		return err
	}
	draft := b.drafts[idx]
	draft.Status = StatusRejected
	draft.RejectedBy = by
	draft.RejectedAt = now
	draft.RejectComment = comment
	drafts := append([]Draft(nil), b.drafts...)
	drafts[idx] = draft
	return b.commit(drafts)
}

// Publish turns an approved draft into a new grade version. It is idempotent:
// republishing an already-published draft returns its existing version.
func (b *Box) Publish(id uuid.UUID, now int64) (RuleVersion, bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	idx := -1
	for i := range b.drafts {
		if b.drafts[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return RuleVersion{}, false, errors.New("draft not found")
	}
	draft := b.drafts[idx]
	if draft.Status == StatusPublished {
		version, ok := b.book.Find(draft.PublishedVersion, draft.Grade)
		if !ok {
			return RuleVersion{}, true, errors.New("published version is missing")
		}
		return version, true, nil
	}
	if draft.Status != StatusApproved {
		return RuleVersion{}, false, errors.New("draft is not approved")
	}
	before := b.book.snapshot()
	version := b.book.publish(draft.Grade, draft.Candidate, draft.ApprovedBy, draft.ID, now)
	draft.Status = StatusPublished
	draft.PublishedVersion = version.Number
	drafts := append([]Draft(nil), b.drafts...)
	drafts[idx] = draft
	if err := b.persist(drafts); err != nil {
		b.book.replace(before)
		return RuleVersion{}, false, err
	}
	b.drafts = drafts
	return version, false, nil
}

func (b *Box) openDraftIndex(id uuid.UUID) (int, error) {
	for i := range b.drafts {
		if b.drafts[i].ID == id {
			if b.drafts[i].Status != StatusDraft {
				return -1, errors.New("draft is not open")
			}
			return i, nil
		}
	}
	return -1, errors.New("draft not found")
}

// persist writes drafts together with the current book. Callers hold b.mu.
func (b *Box) persist(drafts []Draft) error {
	return b.store.Save(Snapshot{Versions: b.book.snapshot(), Drafts: drafts})
}

// commit persists and adopts the drafts slice on success. Callers hold b.mu.
func (b *Box) commit(drafts []Draft) error {
	if err := b.persist(drafts); err != nil {
		return err
	}
	b.drafts = drafts
	return nil
}
