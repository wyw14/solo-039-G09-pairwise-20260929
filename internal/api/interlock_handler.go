package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/wyw14/solo-039/internal/service"
)

type ruleDraftInput struct {
	Grade              string   `json:"grade"`
	MinimumEvidence    int      `json:"minimum_evidence"`
	RequireIndependent *bool    `json:"require_independent"`
	RequiredSources    []string `json:"required_sources"`
	AllowedPhases      []string `json:"allowed_phases"`
	EffectiveAt        int64    `json:"effective_at"`
	ExpiresAt          int64    `json:"expires_at"`
	Rationale          string   `json:"rationale"`
	CreatedBy          string   `json:"created_by"`
}

func (s *Server) listRuleDrafts(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"items": s.plant.ListRuleDrafts()})
}

func (s *Server) createRuleDraft(w http.ResponseWriter, r *http.Request) {
	var input ruleDraftInput
	if err := decodeJSON(r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	// 缺省视为需要独立证据，与出厂基线一致；显式 false 才能放宽。
	independent := true
	if input.RequireIndependent != nil {
		independent = *input.RequireIndependent
	}
	req := service.RuleDraftRequest{
		Grade:              input.Grade,
		MinimumEvidence:    input.MinimumEvidence,
		RequireIndependent: independent,
		RequiredSources:    input.RequiredSources,
		AllowedPhases:      input.AllowedPhases,
		EffectiveAt:        input.EffectiveAt,
		ExpiresAt:          input.ExpiresAt,
		Rationale:          input.Rationale,
		CreatedBy:          input.CreatedBy,
	}
	draft, err := s.plant.SubmitRuleDraft(req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, draft)
}

func (s *Server) getRuleDraft(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid draft id"})
		return
	}
	draft, err := s.plant.GetRuleDraft(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, draft)
}

func (s *Server) replayRuleDraft(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid draft id"})
		return
	}
	summary, err := s.plant.ReplayRuleDraft(id)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, summary)
}

type ruleReviewInput struct {
	Approver string `json:"approver"`
	Reason   string `json:"reason"`
}

func (s *Server) approveRuleDraft(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid draft id"})
		return
	}
	var input ruleReviewInput
	if err := decodeJSON(r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	draft, err := s.plant.ApproveRuleDraft(id, input.Approver)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, draft)
}

func (s *Server) rejectRuleDraft(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid draft id"})
		return
	}
	var input ruleReviewInput
	if err := decodeJSON(r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	draft, err := s.plant.RejectRuleDraft(id, input.Approver, input.Reason)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, draft)
}

func (s *Server) listRuleVersions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"versions": s.plant.RuleVersions()})
}

func (s *Server) listInterlockDecisions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"items": s.plant.InterlockDecisions()})
}

func (s *Server) explainInterlockDecision(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid decision id"})
		return
	}
	explanation, err := s.plant.ExplainInterlockDecision(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, explanation)
}
