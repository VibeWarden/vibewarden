package http

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	proposalapp "github.com/vibewarden/vibewarden/internal/app/proposal"
	"github.com/vibewarden/vibewarden/internal/domain/proposal"
	"github.com/vibewarden/vibewarden/internal/ports"
)

// ProposalHandlers provides HTTP handler functions for the proposal lifecycle API.
// All routes are registered under /_vibewarden/admin/proposals.
type ProposalHandlers struct {
	svc    *proposalapp.Service
	logger *slog.Logger
}

// NewProposalHandlers creates a new ProposalHandlers backed by the supplied service.
// logger may be nil; slog.Default() is used when nil.
func NewProposalHandlers(svc *proposalapp.Service, logger *slog.Logger) *ProposalHandlers {
	if logger == nil {
		logger = slog.Default()
	}
	return &ProposalHandlers{svc: svc, logger: logger}
}

// RegisterRoutes registers proposal routes on mux.
func (h *ProposalHandlers) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /_vibewarden/admin/proposals", h.create)
	mux.HandleFunc("GET /_vibewarden/admin/proposals", h.list)
	mux.HandleFunc("GET /_vibewarden/admin/proposals/", h.getOrAction)
	mux.HandleFunc("POST /_vibewarden/admin/proposals/", h.getOrAction)
}

// ------------------------------------------------------------------
// Request / response types
// ------------------------------------------------------------------

// createProposalRequest is the JSON body expected by POST /_vibewarden/admin/proposals.
type createProposalRequest struct {
	ActionType string         `json:"action_type"`
	Params     map[string]any `json:"params"`
	Reason     string         `json:"reason"`
}

// proposalResponse is the JSON representation of a proposal returned by the API.
// The admin_token field is never included here (it only lives inside Params, which
// originates from the MCP tool and is never returned verbatim).
type proposalResponse struct {
	ID        string         `json:"id"`
	Type      string         `json:"type"`
	Params    map[string]any `json:"params"`
	Reason    string         `json:"reason"`
	Diff      string         `json:"diff,omitempty"`
	Status    string         `json:"status"`
	CreatedAt string         `json:"created_at"`
	ExpiresAt string         `json:"expires_at"`
	Source    string         `json:"source"`
}

// listProposalsResponse is the JSON body returned by GET /_vibewarden/admin/proposals.
type listProposalsResponse struct {
	Proposals []proposalResponse `json:"proposals"`
}

// ------------------------------------------------------------------
// Handlers
// ------------------------------------------------------------------

// create handles POST /_vibewarden/admin/proposals.
func (h *ProposalHandlers) create(w http.ResponseWriter, r *http.Request) {
	var req createProposalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
		return
	}

	actionType := proposal.ActionType(req.ActionType)
	switch actionType {
	case proposal.ActionBlockIP, proposal.ActionAdjustRateLimit, proposal.ActionUpdateConfig:
		// valid
	default:
		writeError(w, http.StatusBadRequest, "invalid_action_type",
			"action_type must be one of: block_ip, adjust_rate_limit, update_config")
		return
	}

	if req.Reason == "" {
		writeError(w, http.StatusBadRequest, "missing_reason", "reason is required")
		return
	}

	p, err := h.svc.Create(r.Context(), proposalapp.CreateParams{
		Type:   actionType,
		Params: req.Params,
		Reason: req.Reason,
		Source: proposal.SourceMCPAgent,
	})
	if err != nil {
		h.logger.Error("creating proposal failed", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	writeJSON(w, http.StatusCreated, toProposalResponse(p))
}

// list handles GET /_vibewarden/admin/proposals.
// Optional query parameter: status (pending|approved|dismissed|expired).
func (h *ProposalHandlers) list(w http.ResponseWriter, r *http.Request) {
	var status proposal.Status
	if raw := r.URL.Query().Get("status"); raw != "" {
		s := proposal.Status(raw)
		switch s {
		case proposal.StatusPending, proposal.StatusApproved, proposal.StatusDismissed, proposal.StatusExpired:
			status = s
		default:
			writeError(w, http.StatusBadRequest, "invalid_status",
				"status must be one of: pending, approved, dismissed, expired")
			return
		}
	}

	proposals, err := h.svc.List(r.Context(), status)
	if err != nil {
		h.logger.Error("listing proposals failed", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}

	resp := listProposalsResponse{
		Proposals: make([]proposalResponse, 0, len(proposals)),
	}
	for _, p := range proposals {
		resp.Proposals = append(resp.Proposals, toProposalResponse(p))
	}
	writeJSON(w, http.StatusOK, resp)
}

// getOrAction dispatches GET and POST requests to proposal sub-resources.
// Patterns handled:
//   - GET  /_vibewarden/admin/proposals/{id}         → get
//   - POST /_vibewarden/admin/proposals/{id}/approve → approve
//   - POST /_vibewarden/admin/proposals/{id}/dismiss → dismiss
func (h *ProposalHandlers) getOrAction(w http.ResponseWriter, r *http.Request) {
	const prefix = "/_vibewarden/admin/proposals/"
	tail := strings.TrimPrefix(r.URL.Path, prefix)
	if tail == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "proposal ID is required")
		return
	}

	// Split on / to check for sub-action.
	parts := strings.SplitN(tail, "/", 2)
	id := parts[0]
	action := ""
	if len(parts) == 2 {
		action = parts[1]
	}

	switch {
	case r.Method == http.MethodGet && action == "":
		h.get(w, r, id)
	case r.Method == http.MethodPost && action == "approve":
		h.approve(w, r, id)
	case r.Method == http.MethodPost && action == "dismiss":
		h.dismiss(w, r, id)
	default:
		writeError(w, http.StatusNotFound, "not_found", "route not found")
	}
}

// get handles GET /_vibewarden/admin/proposals/{id}.
func (h *ProposalHandlers) get(w http.ResponseWriter, r *http.Request, id string) {
	p, err := h.svc.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, ports.ErrProposalNotFound) {
			writeError(w, http.StatusNotFound, "proposal_not_found", "proposal not found")
			return
		}
		h.logger.Error("getting proposal failed", slog.String("proposal_id", id), slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, toProposalResponse(p))
}

// approve handles POST /_vibewarden/admin/proposals/{id}/approve.
func (h *ProposalHandlers) approve(w http.ResponseWriter, r *http.Request, id string) {
	p, err := h.svc.Approve(r.Context(), id)
	if err != nil {
		if errors.Is(err, ports.ErrProposalNotFound) {
			writeError(w, http.StatusNotFound, "proposal_not_found", "proposal not found")
			return
		}
		if errors.Is(err, ports.ErrProposalNotPending) {
			writeError(w, http.StatusConflict, "proposal_not_pending", "proposal is not pending")
			return
		}
		h.logger.Error("approving proposal failed", slog.String("proposal_id", id), slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, toProposalResponse(p))
}

// dismiss handles POST /_vibewarden/admin/proposals/{id}/dismiss.
func (h *ProposalHandlers) dismiss(w http.ResponseWriter, r *http.Request, id string) {
	p, err := h.svc.Dismiss(r.Context(), id)
	if err != nil {
		if errors.Is(err, ports.ErrProposalNotFound) {
			writeError(w, http.StatusNotFound, "proposal_not_found", "proposal not found")
			return
		}
		if errors.Is(err, ports.ErrProposalNotPending) {
			writeError(w, http.StatusConflict, "proposal_not_pending", "proposal is not pending")
			return
		}
		h.logger.Error("dismissing proposal failed", slog.String("proposal_id", id), slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, toProposalResponse(p))
}

// ------------------------------------------------------------------
// Helpers
// ------------------------------------------------------------------

// toProposalResponse converts a domain proposal.Proposal to the wire representation.
func toProposalResponse(p proposal.Proposal) proposalResponse {
	return proposalResponse{
		ID:        p.ID,
		Type:      string(p.Type),
		Params:    p.Params,
		Reason:    p.Reason,
		Diff:      p.Diff,
		Status:    string(p.Status),
		CreatedAt: p.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		ExpiresAt: p.ExpiresAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		Source:    p.Source,
	}
}
