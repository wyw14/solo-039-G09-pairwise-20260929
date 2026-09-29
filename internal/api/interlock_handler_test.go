package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRuleDraftLifecycleEndpoints(t *testing.T) {
	srv := NewServer()

	// 初始规则簿应只有出厂基线 v1。
	versions := getJSON(t, srv, "/api/interlock/rules")
	if len(versions["versions"].([]any)) != 1 {
		t.Fatalf("expected one baseline version, got %v", versions["versions"])
	}
	decisions := getJSON(t, srv, "/api/interlock/decisions")
	if len(decisions["items"].([]any)) != 0 {
		t.Fatal("fresh plant should have no interlock decisions")
	}

	body := `{
		"grade": "polymer-a",
		"minimum_evidence": 3,
		"require_independent": true,
		"required_sources": ["analyzer","column-pressure","reflux"],
		"allowed_phases": ["on-spec"],
		"expires_at": 4000000000,
		"rationale": "stricter grade change control",
		"created_by": "engineer-a"
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/interlock/drafts", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create draft status = %d body=%s", rec.Code, rec.Body.String())
	}
	var draft map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &draft); err != nil {
		t.Fatal(err)
	}
	if draft["Status"] != "pending" {
		t.Fatalf("new draft status = %v, want pending", draft["Status"])
	}
	id := draft["ID"].(string)

	// 未批准时规则簿仍只有基线版本。
	versions = getJSON(t, srv, "/api/interlock/rules")
	if len(versions["versions"].([]any)) != 1 {
		t.Fatal("pending draft must not publish a version")
	}

	// 回放（此刻没有历史批次，差异应为 0，但接口应可用）。
	req = httptest.NewRequest(http.MethodPost, "/api/interlock/drafts/"+id+"/replay", nil)
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("replay status = %d body=%s", rec.Code, rec.Body.String())
	}

	// 审批通过后新版本出现。
	approve := `{"approver":"shift-supervisor"}`
	req = httptest.NewRequest(http.MethodPost, "/api/interlock/drafts/"+id+"/approve", strings.NewReader(approve))
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("approve status = %d body=%s", rec.Code, rec.Body.String())
	}
	versions = getJSON(t, srv, "/api/interlock/rules")
	if len(versions["versions"].([]any)) != 2 {
		t.Fatalf("expected two versions after approval, got %v", versions["versions"])
	}

	// 非法阶段应被拒绝。
	bad := `{"grade":"x","minimum_evidence":1,"allowed_phases":["bogus"],"expires_at":4000000000}`
	req = httptest.NewRequest(http.MethodPost, "/api/interlock/drafts", strings.NewReader(bad))
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown phase status = %d, want 400", rec.Code)
	}
}

func getJSON(t *testing.T, srv *Server, path string) map[string]any {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s status = %d body=%s", path, rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}
