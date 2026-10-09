// Package dashboard serves the dashboard's JSON API under /api and, until the
// web app exists, a placeholder page everywhere else. All security decisions
// (login, sessions, throttling) are made here and in package auth, never in
// the browser.
package dashboard

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/leanhanc/tero/internal/auth"
	"github.com/leanhanc/tero/internal/password"
)

const (
	maxBodyBytes = 64 << 10
	recentEvents = 50
	// sessionCookieLifetime matches the absolute session timeout; the server
	// enforces both timeouts whatever the browser keeps.
	sessionCookieLifetime = 12 * time.Hour
)

const placeholderPage = `<!doctype html>
<html lang="en">
<meta charset="utf-8">
<title>Tero</title>
<h1>Tero is running</h1>
<p>The dashboard will appear here.</p>
</html>
`

// Handler is the dashboard for one domain.
type Handler struct {
	auth   *auth.Service
	origin string
	mux    *http.ServeMux
}

// NewHandler returns the dashboard served at https://domain.
func NewHandler(service *auth.Service, domain string) *Handler {
	h := &Handler{auth: service, origin: "https://" + domain, mux: http.NewServeMux()}

	h.mux.HandleFunc("GET /api/state", h.state)
	h.mux.HandleFunc("POST /api/setup/begin", h.beginSetup)
	h.mux.HandleFunc("POST /api/setup/finish", h.finishSetup)
	h.mux.HandleFunc("POST /api/login", h.login)
	h.mux.HandleFunc("POST /api/logout", h.logout)
	h.mux.HandleFunc("GET /api/events", h.requireSession(h.events))
	h.mux.HandleFunc("/api/", h.notFound)
	h.mux.HandleFunc("/", h.page)

	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	isAPI := strings.HasPrefix(r.URL.Path, "/api/")
	if isAPI {
		w.Header().Set("Cache-Control", "no-store")
	}

	if err := h.checkSameOrigin(r); err != nil {
		writeError(w, http.StatusForbidden, err)
		return
	}

	h.mux.ServeHTTP(w, r)
}

// checkSameOrigin refuses state-changing requests from other sites. Together
// with the SameSite=Strict cookie and the JSON-only API, it blocks cross-site
// request forgery: an HTML form can't send application/json.
func (h *Handler) checkSameOrigin(r *http.Request) error {
	isSafeMethod := r.Method == http.MethodGet || r.Method == http.MethodHead
	if isSafeMethod {
		return nil
	}

	origin := r.Header.Get("Origin")
	isCrossOrigin := origin != "" && origin != h.origin
	isCrossSite := r.Header.Get("Sec-Fetch-Site") == "cross-site"
	if isCrossOrigin || isCrossSite {
		return errors.New("Cross-site requests are not allowed")
	}

	mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mediaType != "application/json" {
		return errors.New("Requests must be JSON")
	}

	return nil
}

func (h *Handler) page(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, placeholderPage)
}

func (h *Handler) notFound(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotFound, errors.New("Not found"))
}

type stateResponse struct {
	IsSetUp    bool `json:"isSetUp"`
	IsLoggedIn bool `json:"isLoggedIn"`
}

func (h *Handler) state(w http.ResponseWriter, r *http.Request) {
	isSetUp, err := h.auth.IsSetUp(r.Context())
	if err != nil {
		h.fail(w, r, err)
		return
	}

	isLoggedIn := h.auth.Authenticate(r.Context(), cookieValue(r, auth.SessionCookie)) == nil
	writeJSON(w, http.StatusOK, stateResponse{IsSetUp: isSetUp, IsLoggedIn: isLoggedIn})
}

type beginSetupRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

func (h *Handler) beginSetup(w http.ResponseWriter, r *http.Request) {
	var request beginSetupRequest
	if !readJSON(w, r, &request) {
		return
	}

	enrolment, err := h.auth.BeginSetup(r.Context(), clientIP(r), request.Token, request.Password)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, enrolment)
}

type finishSetupRequest struct {
	Token string `json:"token"`
	Code  string `json:"code"`
}

func (h *Handler) finishSetup(w http.ResponseWriter, r *http.Request) {
	var request finishSetupRequest
	if !readJSON(w, r, &request) {
		return
	}

	result, err := h.auth.FinishSetup(r.Context(), clientIP(r), request.Token, request.Code)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	setLoginCookies(w, result)
	w.WriteHeader(http.StatusNoContent)
}

type loginRequest struct {
	Password string `json:"password"`
	Code     string `json:"code"`
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var request loginRequest
	if !readJSON(w, r, &request) {
		return
	}

	result, err := h.auth.Login(r.Context(), clientIP(r), cookieValue(r, auth.DeviceCookie), request.Password, request.Code)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	setLoginCookies(w, result)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	if err := h.auth.Logout(r.Context(), cookieValue(r, auth.SessionCookie)); err != nil {
		h.fail(w, r, err)
		return
	}

	clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) events(w http.ResponseWriter, r *http.Request) {
	events, err := h.auth.RecentEvents(r.Context(), recentEvents)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"events": events})
}

// requireSession answers 401 unless the request carries a live session.
func (h *Handler) requireSession(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		err := h.auth.Authenticate(r.Context(), cookieValue(r, auth.SessionCookie))
		if errors.Is(err, auth.ErrSessionExpired) {
			clearSessionCookie(w)
			writeError(w, http.StatusUnauthorized, err)
			return
		}
		if err != nil {
			h.fail(w, r, err)
			return
		}

		next(w, r)
	}
}

// fail maps auth errors to responses. Anything unexpected is logged and
// answered with a generic 500, so internals never reach the browser.
func (h *Handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	var limited auth.RateLimitedError
	if errors.As(err, &limited) {
		w.Header().Set("Retry-After", strconv.Itoa(int(limited.RetryAfter.Seconds())))
		writeError(w, http.StatusTooManyRequests, err)
		return
	}
	if errors.Is(err, password.ErrBusy) {
		w.Header().Set("Retry-After", "5")
		writeError(w, http.StatusServiceUnavailable, password.ErrBusy)
		return
	}

	statuses := map[error]int{
		auth.ErrNotSetUp:           http.StatusConflict,
		auth.ErrAlreadySetUp:       http.StatusConflict,
		auth.ErrInvalidLink:        http.StatusForbidden,
		auth.ErrInvalidCredentials: http.StatusUnauthorized,
		auth.ErrInvalidCode:        http.StatusUnprocessableEntity,
		auth.ErrSessionExpired:     http.StatusUnauthorized,
		password.ErrTooShort:       http.StatusUnprocessableEntity,
		password.ErrTooLong:        http.StatusUnprocessableEntity,
		password.ErrBreached:       http.StatusUnprocessableEntity,
	}
	for known, status := range statuses {
		if errors.Is(err, known) {
			writeError(w, status, known)
			return
		}
	}

	if !errors.Is(err, context.Canceled) {
		slog.Error("dashboard request failed", "path", r.URL.Path, "error", err)
	}
	writeError(w, http.StatusInternalServerError, errors.New("Something went wrong on the server"))
}

func setLoginCookies(w http.ResponseWriter, result auth.LoginResult) {
	setCookie(w, auth.SessionCookie, result.SessionToken, sessionCookieLifetime)
	setCookie(w, auth.DeviceCookie, result.DeviceToken, auth.DeviceLifetime)
}

func setCookie(w http.ResponseWriter, name, value string, lifetime time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		MaxAge:   int(lifetime.Seconds()),
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     auth.SessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
}

func cookieValue(r *http.Request, name string) string {
	cookie, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return cookie.Value
}

// clientIP is the TCP peer address. Caddy serves the dashboard in the same
// process with no proxy in front, so forwarding headers are never trusted.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func readJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, errors.New("The request body is not valid JSON"))
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
