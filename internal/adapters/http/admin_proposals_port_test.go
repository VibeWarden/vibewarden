package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	httpadapter "github.com/vibewarden/vibewarden/internal/adapters/http"
	"github.com/vibewarden/vibewarden/internal/domain/proposal"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// stubProposalService is a hand-rolled fake implementation of
// ports.ProposalService used to prove that the HTTP handler depends on the
// inbound port rather than the concrete *proposalapp.Service. It records the
// most recent call arguments and returns caller-configured responses.
type stubProposalService struct {
	// createFn, listFn, getFn, approveFn, dismissFn override behaviour.
	createFn  func(ctx context.Context, params ports.ProposalCreateParams) (proposal.Proposal, error)
	listFn    func(ctx context.Context, status proposal.Status) ([]proposal.Proposal, error)
	getFn     func(ctx context.Context, id string) (proposal.Proposal, error)
	approveFn func(ctx context.Context, id string) (proposal.Proposal, error)
	dismissFn func(ctx context.Context, id string) (proposal.Proposal, error)

	lastCreateParams ports.ProposalCreateParams
	lastListStatus   proposal.Status
	lastID           string
}

func (s *stubProposalService) Create(ctx context.Context, params ports.ProposalCreateParams) (proposal.Proposal, error) {
	s.lastCreateParams = params
	if s.createFn != nil {
		return s.createFn(ctx, params)
	}
	return proposal.Proposal{}, nil
}

func (s *stubProposalService) List(ctx context.Context, status proposal.Status) ([]proposal.Proposal, error) {
	s.lastListStatus = status
	if s.listFn != nil {
		return s.listFn(ctx, status)
	}
	return nil, nil
}

func (s *stubProposalService) Get(ctx context.Context, id string) (proposal.Proposal, error) {
	s.lastID = id
	if s.getFn != nil {
		return s.getFn(ctx, id)
	}
	return proposal.Proposal{}, ports.ErrProposalNotFound
}

func (s *stubProposalService) Approve(ctx context.Context, id string) (proposal.Proposal, error) {
	s.lastID = id
	if s.approveFn != nil {
		return s.approveFn(ctx, id)
	}
	return proposal.Proposal{}, ports.ErrProposalNotFound
}

func (s *stubProposalService) Dismiss(ctx context.Context, id string) (proposal.Proposal, error) {
	s.lastID = id
	if s.dismissFn != nil {
		return s.dismissFn(ctx, id)
	}
	return proposal.Proposal{}, ports.ErrProposalNotFound
}

func newStubMux(t *testing.T, stub ports.ProposalService) *http.ServeMux {
	t.Helper()
	h := httpadapter.NewProposalHandlers(stub, nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	return mux
}

func TestProposalHandlers_UsesPortNotConcreteService_Create(t *testing.T) {
	now := time.Date(2026, 4, 15, 12, 0, 0, 0, time.UTC)
	stub := &stubProposalService{
		createFn: func(_ context.Context, params ports.ProposalCreateParams) (proposal.Proposal, error) {
			return proposal.Proposal{
				ID:        "stub-id-1",
				Type:      params.Type,
				Params:    params.Params,
				Reason:    params.Reason,
				Status:    proposal.StatusPending,
				CreatedAt: now,
				ExpiresAt: now.Add(proposal.DefaultTTL),
				Source:    proposal.SourceMCPAgent,
			}, nil
		},
	}
	mux := newStubMux(t, stub)

	body := map[string]any{
		"action_type": "block_ip",
		"params":      map[string]any{"ip": "1.2.3.4"},
		"reason":      "traffic spike",
	}
	raw, _ := json.Marshal(body)
	r := httptest.NewRequest(http.MethodPost, "/_vibewarden/admin/proposals", bytes.NewReader(raw))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusCreated {
		t.Fatalf("POST status = %d, want 201; body: %s", w.Code, w.Body.String())
	}

	if stub.lastCreateParams.Type != proposal.ActionBlockIP {
		t.Errorf("stub.lastCreateParams.Type = %q, want %q",
			stub.lastCreateParams.Type, proposal.ActionBlockIP)
	}
	if stub.lastCreateParams.Reason != "traffic spike" {
		t.Errorf("stub.lastCreateParams.Reason = %q, want %q",
			stub.lastCreateParams.Reason, "traffic spike")
	}
	if stub.lastCreateParams.Source != proposal.SourceMCPAgent {
		t.Errorf("stub.lastCreateParams.Source = %q, want %q",
			stub.lastCreateParams.Source, proposal.SourceMCPAgent)
	}
}

func TestProposalHandlers_UsesPortNotConcreteService_GetNotFound(t *testing.T) {
	stub := &stubProposalService{
		getFn: func(_ context.Context, _ string) (proposal.Proposal, error) {
			return proposal.Proposal{}, ports.ErrProposalNotFound
		},
	}
	mux := newStubMux(t, stub)

	r := httptest.NewRequest(http.MethodGet, "/_vibewarden/admin/proposals/does-not-exist", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("GET status = %d, want 404", w.Code)
	}
	if stub.lastID != "does-not-exist" {
		t.Errorf("stub.lastID = %q, want %q", stub.lastID, "does-not-exist")
	}
}

func TestProposalHandlers_UsesPortNotConcreteService_ApproveNotPending(t *testing.T) {
	stub := &stubProposalService{
		approveFn: func(_ context.Context, _ string) (proposal.Proposal, error) {
			return proposal.Proposal{}, ports.ErrProposalNotPending
		},
	}
	mux := newStubMux(t, stub)

	r := httptest.NewRequest(http.MethodPost, "/_vibewarden/admin/proposals/some-id/approve", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusConflict {
		t.Fatalf("POST approve status = %d, want 409", w.Code)
	}
}

func TestProposalHandlers_UsesPortNotConcreteService_InternalError(t *testing.T) {
	stub := &stubProposalService{
		createFn: func(_ context.Context, _ ports.ProposalCreateParams) (proposal.Proposal, error) {
			return proposal.Proposal{}, errors.New("backing store offline")
		},
	}
	mux := newStubMux(t, stub)

	body := map[string]any{
		"action_type": "block_ip",
		"params":      map[string]any{"ip": "1.2.3.4"},
		"reason":      "traffic spike",
	}
	raw, _ := json.Marshal(body)
	r := httptest.NewRequest(http.MethodPost, "/_vibewarden/admin/proposals", bytes.NewReader(raw))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("POST on service error status = %d, want 500", w.Code)
	}
}
