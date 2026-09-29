package api

import (
	"errors"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/wyw14/solo-039/internal/model"
	"github.com/wyw14/solo-039/internal/sandbox"
	"net/http"
	"strconv"
	"strings"
)

type createDraftRequest struct {
	Grade              string   `json:"grade"`
	MinimumEvidence    int      `json:"minimum_evidence"`
	RequireIndependent *bool    `json:"require_independent"`
	AllowedPhases      []string `json:"allowed_phases"`
	RequiredSources    []string `json:"required_sources"`
	ExpiresAt          int64    `json:"expires_at"`
	Reason             string   `json:"reason"`
	CreatedBy          string   `json:"created_by"`
}

type decisionRequest struct {
	By      string `json:"by"`
	Comment string `json:"comment"`
}

func (s *Server) createRuleDraft(w http.ResponseWriter, r *http.Request) {
	var input createDraftRequest
	if err := decodeJSON(r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	spec, err := buildRuleSpec(input.MinimumEvidence, input.RequireIndependent, input.AllowedPhases, input.RequiredSources)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	draft, err := s.plant.CreateRuleDraft(sandbox.DraftInput{
		Grade:     input.Grade,
		Spec:      spec,
		ExpiresAt: input.ExpiresAt,
		Reason:    input.Reason,
		CreatedBy: input.CreatedBy,
	})
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusAccepted, draft)
}

func buildRuleSpec(minimum int, requireIndependent *bool, phaseNames []string, sources []string) (sandbox.RuleSpec, error) {
	if minimum == 0 {
		minimum = 2
	}
	independent := true
	if requireIndependent != nil {
		independent = *requireIndependent
	}
	if len(phaseNames) == 0 {
		phaseNames = []string{string(model.OnSpec)}
	}
	known := make(map[model.Phase]struct{}, len(model.Phases()))
	for _, phase := range model.Phases() {
		known[phase] = struct{}{}
	}
	phases := make([]model.Phase, 0, len(phaseNames))
	for _, name := range phaseNames {
		phase := model.Phase(strings.TrimSpace(name))
		if _, ok := known[phase]; !ok {
			return sandbox.RuleSpec{}, errors.New("unknown allowed phase: " + name)
		}
		phases = append(phases, phase)
	}
	return sandbox.RuleSpec{MinimumEvidence: minimum, RequireIndependent: independent, AllowedPhases: phases, RequiredSources: sources}, nil
}

func (s *Server) listRuleDrafts(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"drafts": s.plant.RuleDrafts()})
}

func (s *Server) getRuleDraft(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid draft id"})
		return
	}
	draft, ok := s.plant.RuleDraft(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "draft not found"})
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
	report, err := s.plant.ReplayRuleDraft(id)
	if err != nil {
		writeRuleDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, report)
}

func (s *Server) approveRuleDraft(w http.ResponseWriter, r *http.Request) {
	var input decisionRequest
	if err := decodeJSON(r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if input.By == "" {
		input.By = "operator"
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid draft id"})
		return
	}
	draft, err := s.plant.ApproveRuleDraft(id, input.By, input.Comment)
	if err != nil {
		writeRuleDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, draft)
}

func (s *Server) rejectRuleDraft(w http.ResponseWriter, r *http.Request) {
	var input decisionRequest
	if err := decodeJSON(r, &input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if input.By == "" {
		input.By = "operator"
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid draft id"})
		return
	}
	if err := s.plant.RejectRuleDraft(id, input.By, input.Comment); err != nil {
		writeRuleDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "rejected"})
}

func (s *Server) publishRuleDraft(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid draft id"})
		return
	}
	version, alreadyPublished, err := s.plant.PublishRuleDraft(id)
	if err != nil {
		writeRuleDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"version": version, "already_published": alreadyPublished})
}

func (s *Server) listRuleVersions(w http.ResponseWriter, r *http.Request) {
	grade := r.URL.Query().Get("grade")
	writeJSON(w, http.StatusOK, map[string]any{"grade": grade, "versions": s.plant.RuleVersions(grade)})
}

func (s *Server) getRuleVersion(w http.ResponseWriter, r *http.Request) {
	number, err := strconv.Atoi(chi.URLParam(r, "number"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid version number"})
		return
	}
	grade := r.URL.Query().Get("grade")
	version, ok := s.plant.FindRuleVersion(number, grade)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "version not found"})
		return
	}
	writeJSON(w, http.StatusOK, version)
}

func (s *Server) activeRule(w http.ResponseWriter, r *http.Request) {
	grade := r.URL.Query().Get("grade")
	at := int64(0)
	if raw := r.URL.Query().Get("at"); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid at"})
			return
		}
		at = parsed
	}
	resolvedAt := at
	if resolvedAt <= 0 {
		resolvedAt = s.plant.NowUnix()
	}
	version := s.plant.ActiveRule(grade, at)
	writeJSON(w, http.StatusOK, map[string]any{"grade": grade, "at": resolvedAt, "version": version})
}

// writeRuleDomainError maps a missing entity to 404 and every other domain
// state error to 409, matching the existing handler conventions.
func writeRuleDomainError(w http.ResponseWriter, err error) {
	if err == nil {
		return
	}
	if strings.Contains(err.Error(), "not found") {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
}
