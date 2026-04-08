package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	httpadapter "github.com/vibewarden/vibewarden/internal/adapters/http"
	proposaladapter "github.com/vibewarden/vibewarden/internal/adapters/proposal"
	proposalapp "github.com/vibewarden/vibewarden/internal/app/proposal"
	"github.com/vibewarden/vibewarden/internal/domain/proposal"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// noopApplier satisfies ports.ProposalApplier without writing files.
type noopApplier struct{}

func (a *noopApplier) Apply(_ context.Context, _ proposal.Proposal) error { return nil }

func newTestProposalHandlers(t *testing.T) (*httpadapter.ProposalHandlers, *http.ServeMux) {
	t.Helper()
	store := proposaladapter.NewStore()
	svc := proposalapp.NewService(store, &noopApplier{}, nil, nil)
	h := httpadapter.NewProposalHandlers(svc, nil)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	return h, mux
}

func postJSON(mux *http.ServeMux, path string, body any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	r := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func getJSON(mux *http.ServeMux, path string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}

func TestProposalHandlers_Create(t *testing.T) {
	_, mux := newTestProposalHandlers(t)

	body := map[string]any{
		"action_type": "block_ip",
		"params":      map[string]any{"ip": "1.2.3.4"},
		"reason":      "high traffic",
	}

	w := postJSON(mux, "/_vibewarden/admin/proposals", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST /proposals status = %d, want %d; body: %s", w.Code, http.StatusCreated, w.Body.String())
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["id"] == "" || resp["id"] == nil {
		t.Error("response should have non-empty id")
	}
	if resp["status"] != "pending" {
		t.Errorf("status = %v, want pending", resp["status"])
	}
	if resp["type"] != "block_ip" {
		t.Errorf("type = %v, want block_ip", resp["type"])
	}
}

func TestProposalHandlers_Create_InvalidActionType(t *testing.T) {
	_, mux := newTestProposalHandlers(t)

	body := map[string]any{
		"action_type": "invalid_type",
		"params":      map[string]any{},
		"reason":      "test",
	}

	w := postJSON(mux, "/_vibewarden/admin/proposals", body)
	if w.Code != http.StatusBadRequest {
		t.Errorf("POST invalid action_type status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestProposalHandlers_Create_MissingReason(t *testing.T) {
	_, mux := newTestProposalHandlers(t)

	body := map[string]any{
		"action_type": "block_ip",
		"params":      map[string]any{"ip": "1.2.3.4"},
	}

	w := postJSON(mux, "/_vibewarden/admin/proposals", body)
	if w.Code != http.StatusBadRequest {
		t.Errorf("POST missing reason status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestProposalHandlers_List(t *testing.T) {
	_, mux := newTestProposalHandlers(t)

	// Create two proposals.
	postJSON(mux, "/_vibewarden/admin/proposals", map[string]any{
		"action_type": "block_ip",
		"params":      map[string]any{"ip": "1.1.1.1"},
		"reason":      "test 1",
	})
	postJSON(mux, "/_vibewarden/admin/proposals", map[string]any{
		"action_type": "adjust_rate_limit",
		"params":      map[string]any{"requests_per_second": 5.0},
		"reason":      "test 2",
	})

	w := getJSON(mux, "/_vibewarden/admin/proposals")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /proposals status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	proposals, ok := resp["proposals"].([]any)
	if !ok {
		t.Fatalf("proposals not an array: %T", resp["proposals"])
	}
	if len(proposals) != 2 {
		t.Errorf("proposals count = %d, want 2", len(proposals))
	}
}

func TestProposalHandlers_List_FilterByStatus(t *testing.T) {
	_, mux := newTestProposalHandlers(t)

	postJSON(mux, "/_vibewarden/admin/proposals", map[string]any{
		"action_type": "block_ip",
		"params":      map[string]any{"ip": "1.1.1.1"},
		"reason":      "test",
	})

	w := getJSON(mux, "/_vibewarden/admin/proposals?status=pending")
	if w.Code != http.StatusOK {
		t.Fatalf("GET ?status=pending status = %d", w.Code)
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp) //nolint:errcheck
	proposals := resp["proposals"].([]any)
	if len(proposals) != 1 {
		t.Errorf("pending proposals = %d, want 1", len(proposals))
	}
}

func TestProposalHandlers_List_InvalidStatus(t *testing.T) {
	_, mux := newTestProposalHandlers(t)

	w := getJSON(mux, "/_vibewarden/admin/proposals?status=invalid")
	if w.Code != http.StatusBadRequest {
		t.Errorf("GET invalid status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestProposalHandlers_Get(t *testing.T) {
	_, mux := newTestProposalHandlers(t)

	cw := postJSON(mux, "/_vibewarden/admin/proposals", map[string]any{
		"action_type": "block_ip",
		"params":      map[string]any{"ip": "2.2.2.2"},
		"reason":      "test",
	})
	var created map[string]any
	json.NewDecoder(cw.Body).Decode(&created) //nolint:errcheck
	id := created["id"].(string)

	w := getJSON(mux, "/_vibewarden/admin/proposals/"+id)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /proposals/%s status = %d, want %d", id, w.Code, http.StatusOK)
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp) //nolint:errcheck
	if resp["id"] != id {
		t.Errorf("GET id = %v, want %q", resp["id"], id)
	}
}

func TestProposalHandlers_Get_NotFound(t *testing.T) {
	_, mux := newTestProposalHandlers(t)

	w := getJSON(mux, "/_vibewarden/admin/proposals/nonexistent-id")
	if w.Code != http.StatusNotFound {
		t.Errorf("GET nonexistent status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestProposalHandlers_Dismiss(t *testing.T) {
	_, mux := newTestProposalHandlers(t)

	cw := postJSON(mux, "/_vibewarden/admin/proposals", map[string]any{
		"action_type": "block_ip",
		"params":      map[string]any{"ip": "3.3.3.3"},
		"reason":      "test",
	})
	var created map[string]any
	json.NewDecoder(cw.Body).Decode(&created) //nolint:errcheck
	id := created["id"].(string)

	w := postJSON(mux, "/_vibewarden/admin/proposals/"+id+"/dismiss", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("POST dismiss status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp) //nolint:errcheck
	if resp["status"] != "dismissed" {
		t.Errorf("dismiss status = %v, want dismissed", resp["status"])
	}
}

func TestProposalHandlers_Dismiss_NotPending(t *testing.T) {
	_, mux := newTestProposalHandlers(t)

	cw := postJSON(mux, "/_vibewarden/admin/proposals", map[string]any{
		"action_type": "block_ip",
		"params":      map[string]any{"ip": "4.4.4.4"},
		"reason":      "test",
	})
	var created map[string]any
	json.NewDecoder(cw.Body).Decode(&created) //nolint:errcheck
	id := created["id"].(string)

	// Dismiss once.
	postJSON(mux, "/_vibewarden/admin/proposals/"+id+"/dismiss", nil)

	// Dismiss again — should get 409 Conflict.
	w := postJSON(mux, "/_vibewarden/admin/proposals/"+id+"/dismiss", nil)
	if w.Code != http.StatusConflict {
		t.Errorf("double dismiss status = %d, want %d", w.Code, http.StatusConflict)
	}
}

func TestProposalHandlers_Approve_NotFound(t *testing.T) {
	_, mux := newTestProposalHandlers(t)

	w := postJSON(mux, "/_vibewarden/admin/proposals/nonexistent/approve", nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("approve nonexistent status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

// Compile-time check that noopApplier satisfies the interface.
var _ ports.ProposalApplier = (*noopApplier)(nil)
