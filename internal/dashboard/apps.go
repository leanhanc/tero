package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/leanhanc/tero/internal/apps"
	"github.com/leanhanc/tero/internal/deploylog"
	"github.com/leanhanc/tero/internal/deploys"
	"github.com/leanhanc/tero/internal/githubapp"
	"github.com/leanhanc/tero/internal/source"
)

const (
	logPollInterval = 500 * time.Millisecond
	// maxLogStream bounds one log stream; a build stops after 30 minutes.
	maxLogStream = 40 * time.Minute
)

// fieldErrors maps errors from creating an app to the form field they
// belong to.
var fieldErrors = map[error]string{
	source.ErrNotGitHubURL:       "repository",
	source.ErrRepositoryNotFound: "repository",
	source.ErrBranchNotFound:     "branch",
	source.ErrInvalidBranch:      "branch",
	source.ErrFolderNotFound:     "rootDirectory",
	apps.ErrInvalidDisplayName:   "displayName",
	apps.ErrInvalidName:          "name",
	apps.ErrNameTaken:            "name",
	apps.ErrRepositoryNotCovered: "repository",
	githubapp.ErrNotInstalled:    "repository",
}

func (h *Handler) registerAppRoutes() {
	h.mux.HandleFunc("POST /api/repositories/lookup", h.requireSession(h.lookUpRepository))
	h.mux.HandleFunc("GET /api/apps", h.requireSession(h.listApps))
	h.mux.HandleFunc("POST /api/apps", h.requireSession(h.createApp))
	h.mux.HandleFunc("GET /api/apps/{name}", h.requireSession(h.getApp))
	h.mux.HandleFunc("POST /api/apps/{name}/deploys", h.requireSession(h.redeploy))
	h.mux.HandleFunc("POST /api/apps/{name}/settings", h.requireSession(h.changeSettings))
	h.mux.HandleFunc("POST /api/apps/{name}/display-name", h.requireSession(h.changeDisplayName))
	h.mux.HandleFunc("GET /api/deploys/{id}/log", h.requireSession(h.streamLog))
}

type lookupRequest struct {
	URL string `json:"url"`
}

func (h *Handler) lookUpRepository(w http.ResponseWriter, r *http.Request) {
	var request lookupRequest
	if !h.readJSON(w, r, &request) {
		return
	}

	lookup, err := h.apps.LookUpRepository(r.Context(), request.URL)
	if err != nil {
		h.failApps(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, lookup)
}

func (h *Handler) listApps(w http.ResponseWriter, r *http.Request) {
	summaries, err := h.apps.List(r.Context())
	if err != nil {
		h.fail(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"apps": summaries})
}

func (h *Handler) createApp(w http.ResponseWriter, r *http.Request) {
	var request apps.NewApp
	if !h.readJSON(w, r, &request) {
		return
	}

	name, err := h.apps.Create(r.Context(), request)
	if err != nil {
		h.failApps(w, r, err)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]string{"name": name})
}

func (h *Handler) getApp(w http.ResponseWriter, r *http.Request) {
	detail, err := h.apps.Get(r.Context(), r.PathValue("name"))
	if err != nil {
		h.failApps(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, detail)
}

func (h *Handler) redeploy(w http.ResponseWriter, r *http.Request) {
	if err := h.apps.Redeploy(r.Context(), r.PathValue("name")); err != nil {
		h.failApps(w, r, err)
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

// settingsRequest changes the settings it names and leaves the rest.
type settingsRequest struct {
	RunTogether  *bool `json:"runTogether"`
	DeployOnPush *bool `json:"deployOnPush"`
}

func (h *Handler) changeSettings(w http.ResponseWriter, r *http.Request) {
	var request settingsRequest
	if !h.readJSON(w, r, &request) {
		return
	}

	name := r.PathValue("name")
	if request.RunTogether != nil {
		if err := h.apps.SetRunTogether(r.Context(), name, *request.RunTogether); err != nil {
			h.failApps(w, r, err)
			return
		}
	}
	if request.DeployOnPush != nil {
		if err := h.apps.SetDeployOnPush(r.Context(), name, *request.DeployOnPush); err != nil {
			h.failApps(w, r, err)
			return
		}
	}

	w.WriteHeader(http.StatusNoContent)
}

type displayNameRequest struct {
	DisplayName string `json:"displayName"`
}

func (h *Handler) changeDisplayName(w http.ResponseWriter, r *http.Request) {
	var request displayNameRequest
	if !h.readJSON(w, r, &request) {
		return
	}

	if err := h.apps.SetDisplayName(r.Context(), r.PathValue("name"), request.DisplayName); err != nil {
		h.failApps(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// streamLog sends a deploy's build log as Server-Sent Events: what's already
// written, then new output as the agent writes it, then a "done" event with
// the deploy's final state. Each chunk is a JSON string, so newlines survive
// the event format. The browser shows it as text, never as markup.
func (h *Handler) streamLog(w http.ResponseWriter, r *http.Request) {
	deployID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, apps.ErrNotFound)
		return
	}
	if _, err := h.apps.DeployState(r.Context(), deployID); err != nil {
		h.failApps(w, r, err)
		return
	}

	// The response controller finds Flush through Caddy's wrappers.
	flusher := http.NewResponseController(w)

	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)

	ctx, cancel := context.WithTimeout(r.Context(), maxLogStream)
	defer cancel()

	if err := h.followLog(ctx, w, flusher, deployID); err != nil && ctx.Err() == nil {
		writeEvent(w, "problem", "The log couldn't be read")
		_ = flusher.Flush()
	}
}

func (h *Handler) followLog(ctx context.Context, w io.Writer, flusher *http.ResponseController, deployID int64) error {
	var offset int64
	for {
		state, err := h.apps.DeployState(ctx, deployID)
		if err != nil {
			return err
		}
		// Read after the state, so output written before a deploy ended is
		// always sent before "done".
		chunk, err := readFrom(deploylog.Path(h.logDir, deployID), offset)
		if err != nil {
			return err
		}

		if len(chunk) > 0 {
			offset += int64(len(chunk))
			writeEvent(w, "log", string(chunk))
		}

		isFinished := state != deploys.Queued && state != deploys.Building && state != deploys.Starting
		if isFinished {
			writeEvent(w, "done", string(state))
			return flusher.Flush()
		}
		if err := flusher.Flush(); err != nil {
			return err
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(logPollInterval):
		}
	}
}

// readFrom returns the file's content from offset on, or nothing while the
// agent hasn't created it yet.
func readFrom(path string, offset int64) ([]byte, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()

	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		return nil, err
	}

	return io.ReadAll(io.LimitReader(file, deploylog.MaxSize))
}

func writeEvent(w io.Writer, event, data string) {
	encoded, _ := json.Marshal(data)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, encoded)
}

// failApps answers errors from the apps service, pointing form errors at
// their field.
func (h *Handler) failApps(w http.ResponseWriter, r *http.Request, err error) {
	for known, field := range fieldErrors {
		if errors.Is(err, known) {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": known.Error(), "field": field})
			return
		}
	}

	if errors.Is(err, apps.ErrNotFound) {
		writeError(w, http.StatusNotFound, apps.ErrNotFound)
		return
	}
	if errors.Is(err, apps.ErrAgentNotAnswering) {
		writeError(w, http.StatusServiceUnavailable, apps.ErrAgentNotAnswering)
		return
	}
	h.fail(w, r, err)
}
