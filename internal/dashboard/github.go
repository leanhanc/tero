package dashboard

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/leanhanc/tero/internal/auth"
	"github.com/leanhanc/tero/internal/githubapp"
	"github.com/leanhanc/tero/internal/keys"
	"github.com/leanhanc/tero/internal/source"
)

// GitHub is the dashboard's side of the GitHub App: connecting it, and the
// webhooks GitHub sends.
type GitHub struct {
	Connection githubapp.Connection
	Webhooks   githubapp.Webhooks
	Verifier   WebhookVerifier
}

// WebhookVerifier checks a webhook's signature. The key service holds the
// secret, so the dashboard only learns whether it matched.
type WebhookVerifier interface {
	VerifyWebhook(ctx context.Context, body []byte, signature, ip string) (bool, error)
}

// Errors the GitHub section shows.
var (
	errGitHubConnected   = errors.New("GitHub is already connected")
	errInvalidConnection = errors.New("This GitHub connection expired or wasn't started from this browser. Start again from Settings")
)

func (h *Handler) registerGitHubRoutes() {
	h.mux.HandleFunc("GET /api/github", h.requireSession(h.gitHubStatus))
	h.mux.HandleFunc("POST /api/github/connect/begin", h.requireSession(h.beginGitHubConnection))
	h.mux.HandleFunc("POST /api/github/connect", h.requireSession(h.finishGitHubConnection))
	h.mux.HandleFunc("POST /api/github/refresh", h.requireSession(h.refreshGitHub))
	h.mux.HandleFunc("POST /api/github/disconnect", h.requireSession(h.disconnectGitHub))
	h.mux.HandleFunc("GET /api/github/requests/{id}", h.requireSession(h.gitHubRequest))
	h.mux.HandleFunc("POST /api/github/webhook", h.receiveWebhook)
}

func (h *Handler) gitHubStatus(w http.ResponseWriter, r *http.Request) {
	status, err := h.github.Connection.Status(r.Context())
	if err != nil {
		h.failGitHub(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, status)
}

type beginGitHubRequest struct {
	Organization string `json:"organization"`
}

func (h *Handler) beginGitHubConnection(w http.ResponseWriter, r *http.Request) {
	var request beginGitHubRequest
	if !h.readJSON(w, r, &request) {
		return
	}

	start, err := h.github.Connection.Begin(r.Context(), sessionToken(r), request.Organization)
	if err != nil {
		h.failGitHub(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, start)
}

type finishGitHubRequest struct {
	Code  string `json:"code"`
	State string `json:"state"`
}

type gitHubRequestResponse struct {
	RequestID int64 `json:"requestId"`
}

func (h *Handler) finishGitHubConnection(w http.ResponseWriter, r *http.Request) {
	var request finishGitHubRequest
	if !h.readJSON(w, r, &request) {
		return
	}

	requestID, err := h.github.Connection.Finish(r.Context(), sessionToken(r), request.Code, request.State)
	if err != nil {
		h.failGitHub(w, r, err)
		return
	}

	writeJSON(w, http.StatusAccepted, gitHubRequestResponse{RequestID: requestID})
}

func (h *Handler) refreshGitHub(w http.ResponseWriter, r *http.Request) {
	requestID, err := h.github.Connection.Refresh(r.Context())
	if err != nil {
		h.failGitHub(w, r, err)
		return
	}

	writeJSON(w, http.StatusAccepted, gitHubRequestResponse{RequestID: requestID})
}

func (h *Handler) disconnectGitHub(w http.ResponseWriter, r *http.Request) {
	err := h.github.Connection.Disconnect(r.Context(), sessionToken(r))
	if err != nil {
		h.failGitHub(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

type gitHubAnswerResponse struct {
	Status githubapp.RequestStatus `json:"status"`
	Result string                  `json:"result"`
}

func (h *Handler) gitHubRequest(w http.ResponseWriter, r *http.Request) {
	requestID, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, githubapp.ErrRequestNotFound)
		return
	}

	answer, err := h.github.Connection.RequestAnswer(r.Context(), requestID)
	if err != nil {
		h.failGitHub(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, gitHubAnswerResponse{Status: answer.Status, Result: answer.Result})
}

// failGitHub answers errors from connecting GitHub, mapping the key
// service's to the dashboard's.
func (h *Handler) failGitHub(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, keys.ErrNotConfirmed):
		h.fail(w, r, auth.ErrNotConfirmed)
	case errors.Is(err, keys.ErrSessionEnded):
		h.fail(w, r, auth.ErrSessionExpired)
	case errors.Is(err, keys.ErrUnavailable):
		h.fail(w, r, auth.ErrKeysUnavailable)
	case errors.Is(err, keys.ErrGitHubConnected):
		writeError(w, http.StatusConflict, errGitHubConnected)
	case errors.Is(err, keys.ErrInvalidConnection):
		writeError(w, http.StatusForbidden, errInvalidConnection)
	case errors.Is(err, source.ErrInvalidOwner):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error(), "field": "organization"})
	case errors.Is(err, githubapp.ErrRequestNotFound):
		writeError(w, http.StatusNotFound, err)
	default:
		h.fail(w, r, err)
	}
}

// The webhook endpoint is the dashboard's only unauthenticated POST besides
// login and setup. GitHub signs every delivery; the key service checks the
// signature, and nothing in a delivery is trusted beyond what it triggers.
const (
	// badSignatureLimit invalid signatures from one address within a minute
	// make it wait out the minute, without asking the key service.
	badSignatureLimit  = 10
	badSignatureWindow = time.Minute
)

// handledWebhooks are the events Tero acts on. Others are acknowledged
// without being checked.
var handledWebhooks = map[string]bool{"ping": true, "push": true, "installation": true, "installation_repositories": true}

// deliveryPattern is the shape of GitHub's delivery GUID.
var deliveryPattern = regexp.MustCompile(`^[0-9a-fA-F-]{1,64}$`)

var errBadSignature = errors.New("The webhook signature doesn't match")

func (h *Handler) receiveWebhook(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if h.badSignatures.isLimited(ip, time.Now()) {
		w.Header().Set("Retry-After", strconv.Itoa(int(badSignatureWindow.Seconds())))
		writeError(w, http.StatusTooManyRequests, errBadSignature)
		return
	}

	event := r.Header.Get("X-GitHub-Event")
	guid := r.Header.Get("X-GitHub-Delivery")
	signature := r.Header.Get("X-Hub-Signature-256")
	if event == "" || !deliveryPattern.MatchString(guid) || signature == "" {
		writeError(w, http.StatusBadRequest, errors.New("A GitHub webhook needs its event, delivery and signature headers"))
		return
	}
	if !handledWebhooks[event] {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, keys.MaxWebhookBody))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, errors.New("The webhook is too large"))
		return
	}

	isValid, err := h.github.Verifier.VerifyWebhook(r.Context(), body, signature, ip)
	if err != nil {
		h.badSignatures.count(ip, time.Now())
		h.fail(w, r, err)
		return
	}
	if !isValid {
		h.badSignatures.count(ip, time.Now())
		writeError(w, http.StatusUnauthorized, errBadSignature)
		return
	}

	isNew, err := h.github.Webhooks.Handle(r.Context(), event, guid, body)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if !isNew {
		w.WriteHeader(http.StatusOK)
		return
	}

	w.WriteHeader(http.StatusAccepted)
}

// badSignatures counts invalid webhook signatures per address in the
// current window.
type badSignatures struct {
	mutex       sync.Mutex
	windowStart time.Time
	counts      map[string]int
}

func (b *badSignatures) isLimited(ip string, now time.Time) bool {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	b.resetIfOver(now)

	return b.counts[senderKey(ip)] >= badSignatureLimit
}

func (b *badSignatures) count(ip string, now time.Time) {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	b.resetIfOver(now)

	b.counts[senderKey(ip)]++
}

// senderKey groups IPv6 senders by /64, since one host usually gets a whole
// /64 and could otherwise rotate addresses past the limit.
func senderKey(ip string) string {
	address, err := netip.ParseAddr(ip)
	if err != nil || !address.Is6() || address.Is4In6() {
		return ip
	}

	prefix, _ := address.Prefix(64)
	return prefix.String()
}

// resetIfOver starts a new window, which also drops every address counted
// in the last, so the map can't grow past one window's senders.
func (b *badSignatures) resetIfOver(now time.Time) {
	if b.counts == nil || now.Sub(b.windowStart) >= badSignatureWindow {
		b.windowStart = now
		b.counts = map[string]int{}
	}
}
