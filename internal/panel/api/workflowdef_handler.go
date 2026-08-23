package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/ygrip/punakawan/internal/workflowdef"
)

// WorkflowRootResolver maps a {projectId} path value to the workspace root
// whose .punakawan/workflows/ directory holds that project's definitions. It
// is injected rather than importing internal/panel/registry so the handlers
// stay decoupled from how projects resolve to roots. Returning a non-nil error
// answers 404 (unknown project).
type WorkflowRootResolver func(projectID string) (root string, err error)

// WorkflowDefHandlers bundles the workflow-definition HTTP handlers and their
// dependencies. The integrator constructs one via NewWorkflowDefHandlers and
// mounts each method on the panel's ServeMux (see the package doc / the
// integrator notes for the exact route table).
//
// Dependencies:
//   - resolveRoot: {projectId} -> workspace root (404 on unknown project).
//   - caps: the registered capability set used to validate definitions and
//     to re-check on invoke. Built with workflowdef.NewCapabilitySet(
//     mcpserver.CapabilityRegistry(a).Names(), adapterOps) - the live MCP
//     tool registry, not a hand-maintained list.
//   - newInvoker: builds an Invoker for a resolved (projectID, root). The
//     integrator wires the RunCreator here so it can bind the workflow-run
//     store for that project; it takes both the project id (the runtime pool's
//     key for non-primary projects) and the root so run creation can target the
//     right workspace. caps is passed through for the invoke-time re-check.
type WorkflowDefHandlers struct {
	resolveRoot WorkflowRootResolver
	caps        workflowdef.CapabilitySet
	newInvoker  func(projectID, root string) workflowdef.Invoker
}

// NewWorkflowDefHandlers constructs the handler bundle. resolveRoot and
// newInvoker must be non-nil.
func NewWorkflowDefHandlers(resolveRoot WorkflowRootResolver, caps workflowdef.CapabilitySet, newInvoker func(projectID, root string) workflowdef.Invoker) *WorkflowDefHandlers {
	return &WorkflowDefHandlers{resolveRoot: resolveRoot, caps: caps, newInvoker: newInvoker}
}

// openStore resolves {projectId} to a root and opens its definition store,
// writing the appropriate error response and returning ok=false on failure.
func (h *WorkflowDefHandlers) openStore(w http.ResponseWriter, r *http.Request) (*workflowdef.Store, bool) {
	root, err := h.resolveRoot(r.PathValue("projectId"))
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return nil, false
	}
	store, err := workflowdef.Open(root)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return nil, false
	}
	return store, true
}

// writeWorkflowValidationError maps a workflowdef validation/store error to the
// documented status + machine code. Unknown/command errors are 400; a revision
// conflict is 409; anything else is 500.
func writeWorkflowValidationError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, workflowdef.ErrUnknownCapability):
		writeCodeError(w, http.StatusBadRequest, "unknown_capability", err)
	case errors.Is(err, workflowdef.ErrCommandNotAllowed):
		writeCodeError(w, http.StatusBadRequest, "command_not_allowed", err)
	case errors.Is(err, workflowdef.ErrBadVersion),
		errors.Is(err, workflowdef.ErrMissingField),
		errors.Is(err, workflowdef.ErrDuplicateStepID),
		errors.Is(err, workflowdef.ErrUnknownStepRef):
		writeCodeError(w, http.StatusBadRequest, "invalid", err)
	case errors.Is(err, workflowdef.ErrRevisionConflict):
		writeCodeError(w, http.StatusConflict, "revision_conflict", err)
	default:
		writeError(w, http.StatusInternalServerError, err)
	}
}

// List serves GET /api/v1/projects/{projectId}/workflows.
func (h *WorkflowDefHandlers) List() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		store, ok := h.openStore(w, r)
		if !ok {
			return
		}
		defs, err := store.List()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		if defs == nil {
			defs = []workflowdef.Definition{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": defs})
	}
}

// Get serves GET /api/v1/projects/{projectId}/workflows/{workflowId}.
func (h *WorkflowDefHandlers) Get() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		store, ok := h.openStore(w, r)
		if !ok {
			return
		}
		def, err := store.Get(r.PathValue("workflowId"))
		if errors.Is(err, workflowdef.ErrNotFound) {
			writeError(w, http.StatusNotFound, err)
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, def)
	}
}

// Create serves POST /api/v1/projects/{projectId}/workflows. The body is a
// full Definition. It is validated against the capability set before being
// saved; validation failures answer 400 with a machine code, a stale revision
// answers 409.
func (h *WorkflowDefHandlers) Create() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		store, ok := h.openStore(w, r)
		if !ok {
			return
		}
		var def workflowdef.Definition
		if err := json.NewDecoder(r.Body).Decode(&def); err != nil {
			writeCodeError(w, http.StatusBadRequest, "invalid", err)
			return
		}
		if err := workflowdef.Validate(def, h.caps); err != nil {
			writeWorkflowValidationError(w, err)
			return
		}
		saved, err := store.Save(def)
		if err != nil {
			writeWorkflowValidationError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, saved)
	}
}

// Enable serves POST /api/v1/projects/{projectId}/workflows/{workflowId}/enable.
func (h *WorkflowDefHandlers) Enable() http.HandlerFunc {
	return h.setEnabled(true)
}

// Disable serves POST /api/v1/projects/{projectId}/workflows/{workflowId}/disable.
func (h *WorkflowDefHandlers) Disable() http.HandlerFunc {
	return h.setEnabled(false)
}

func (h *WorkflowDefHandlers) setEnabled(enabled bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		store, ok := h.openStore(w, r)
		if !ok {
			return
		}
		def, err := store.SetEnabled(r.PathValue("workflowId"), enabled)
		if errors.Is(err, workflowdef.ErrNotFound) {
			writeError(w, http.StatusNotFound, err)
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		writeJSON(w, http.StatusOK, def)
	}
}

// Invoke serves POST /api/v1/projects/{projectId}/workflows/{workflowId}/invoke.
// The body is {"inputs": {...}}; the resolved definition is re-validated and
// its run created via the injected Invoker, returning {"run_id": ...}.
func (h *WorkflowDefHandlers) Invoke() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID := r.PathValue("projectId")
		root, err := h.resolveRoot(projectID)
		if err != nil {
			writeError(w, http.StatusNotFound, err)
			return
		}
		store, err := workflowdef.Open(root)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		def, err := store.Get(r.PathValue("workflowId"))
		if errors.Is(err, workflowdef.ErrNotFound) {
			writeError(w, http.StatusNotFound, err)
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}

		var body struct {
			Inputs map[string]any `json:"inputs"`
		}
		if r.Body != nil {
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
				writeCodeError(w, http.StatusBadRequest, "invalid", err)
				return
			}
		}

		runID, err := h.newInvoker(projectID, root).Invoke(r.Context(), def, body.Inputs)
		if errors.Is(err, workflowdef.ErrDisabled) {
			writeCodeError(w, http.StatusConflict, "disabled", err)
			return
		}
		if err != nil {
			writeWorkflowValidationError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"run_id": runID})
	}
}
