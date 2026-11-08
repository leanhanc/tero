package dashboard

import (
	"errors"
	"net/http"

	"github.com/leanhanc/tero/internal/apps"
	"github.com/leanhanc/tero/internal/auth"
	"github.com/leanhanc/tero/internal/keys"
)

// The dashboard stores no variable. Values pass through it on their way to the
// key service, which acts only for a live session, and for a confirmed one on an
// app with a sealed variable. It records only which names changed.

// maxVariablesBody fits an app's whole allowance of values, which JSON can make
// several times larger by escaping. The key service enforces the limits.
const maxVariablesBody = 1536 << 10

var errKeyNotLoaded = errors.New("Some of this app's variables were sealed with a master key that was lost. Set them again")

func (h *Handler) registerVariableRoutes() {
	h.mux.HandleFunc("GET /api/apps/{name}/variables", h.requireSession(h.listVariables))
	h.mux.HandleFunc("POST /api/apps/{name}/variables", h.requireSession(h.saveVariables))
	h.mux.HandleFunc("POST /api/apps/{name}/variables/redeploy", h.requireSession(h.redeployVariables))
	h.mux.HandleFunc("POST /api/apps/{name}/variables/approve-source", h.requireSession(h.approveSource))
	h.mux.HandleFunc("GET /api/apps/{name}/variables/{variable}/reads", h.requireSession(h.variableReads))
}

func sourceOf(detail apps.Detail) keys.AppSource {
	return keys.AppSource{Repository: detail.Repository, Branch: detail.Branch, RootDirectory: detail.RootDirectory}
}

// variablesResponse is an app's list. ApprovedSource is empty until the first
// variable is saved; Source is what the app builds from now.
type variablesResponse struct {
	Variables        []keys.Variable `json:"variables"`
	ApprovedSource   keys.AppSource  `json:"approvedSource"`
	Source           keys.AppSource  `json:"source"`
	IsSourceApproved bool            `json:"isSourceApproved"`
}

func (h *Handler) listVariables(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	detail, err := h.apps.Get(r.Context(), name)
	if err != nil {
		h.failApps(w, r, err)
		return
	}

	listed, err := h.keys.ListVariables(r.Context(), sessionToken(r), name)
	if err != nil {
		h.failVariables(w, r, err)
		return
	}

	source := sourceOf(detail)
	isApproved := len(listed.Variables) == 0 || listed.Source == source
	writeJSON(w, http.StatusOK, variablesResponse{
		Variables: listed.Variables, ApprovedSource: listed.Source, Source: source, IsSourceApproved: isApproved,
	})
}

type saveVariablesRequest struct {
	Changes []keys.VariableChange `json:"changes"`
}

// saveVariables stores changes and deploys nothing: the app page then offers to
// redeploy them.
func (h *Handler) saveVariables(w http.ResponseWriter, r *http.Request) {
	var request saveVariablesRequest
	if !h.readJSONUpTo(w, r, maxVariablesBody, &request) {
		return
	}

	name := r.PathValue("name")
	detail, err := h.apps.Get(r.Context(), name)
	if err != nil {
		h.failApps(w, r, err)
		return
	}

	saved, err := h.keys.SaveVariables(r.Context(), sessionToken(r), clientIP(r), name, sourceOf(detail), request.Changes)
	if err != nil {
		h.failVariables(w, r, err)
		return
	}

	pending := make([]apps.PendingChange, 0, len(saved.Changed))
	for _, changed := range saved.Changed {
		pending = append(pending, apps.PendingChange{Name: changed.Name, TouchesBuild: changed.TouchesBuild})
	}
	if err := h.apps.RecordPendingChanges(r.Context(), name, pending, saved.Count > 0); err != nil {
		h.failApps(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) redeployVariables(w http.ResponseWriter, r *http.Request) {
	if err := h.apps.RedeployChanges(r.Context(), r.PathValue("name")); err != nil {
		h.failApps(w, r, err)
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

// approveSource approves what the app builds from now, as the page shows it.
func (h *Handler) approveSource(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	detail, err := h.apps.Get(r.Context(), name)
	if err != nil {
		h.failApps(w, r, err)
		return
	}

	if err := h.keys.ApproveSource(r.Context(), sessionToken(r), clientIP(r), name, sourceOf(detail)); err != nil {
		h.failVariables(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) variableReads(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if _, err := h.apps.Get(r.Context(), name); err != nil {
		h.failApps(w, r, err)
		return
	}

	reads, err := h.keys.VariableReads(r.Context(), sessionToken(r), name, r.PathValue("variable"))
	if err != nil {
		h.failVariables(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, reads)
}

func (h *Handler) failVariables(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, keys.ErrNotConfirmed):
		h.fail(w, r, auth.ErrNotConfirmed)
	case errors.Is(err, keys.ErrSessionEnded):
		h.fail(w, r, auth.ErrSessionExpired)
	case errors.Is(err, keys.ErrUnavailable):
		h.fail(w, r, auth.ErrKeysUnavailable)
	case errors.Is(err, keys.ErrInvalidVariable), errors.Is(err, keys.ErrTooManyVariables), errors.Is(err, keys.ErrRefused):
		writeError(w, http.StatusUnprocessableEntity, errInvalidVariables)
	case errors.Is(err, keys.ErrVariableSealed):
		writeError(w, http.StatusConflict, errVariableSealed)
	case errors.Is(err, keys.ErrKeyNotLoaded):
		writeError(w, http.StatusConflict, errKeyNotLoaded)
	default:
		h.fail(w, r, err)
	}
}

var (
	errInvalidVariables = errors.New("Check the variables. Names use letters, digits and underscores and can't start with a digit, PORT is set by Tero, a value holds at most 32 KiB, and an app holds at most 200 variables and 256 KiB. A RAILPACK_ variable can't be sealed")
	errVariableSealed   = errors.New("A sealed variable can only be replaced or deleted")
)
