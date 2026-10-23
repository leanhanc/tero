// Package dashboard serves the dashboard's JSON API under /api and the web
// app everywhere else. All security decisions (login, sessions, throttling)
// are made here and in package auth, never in the browser.
package dashboard

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"rsc.io/qr"

	"github.com/leanhanc/tero/internal/apps"
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

// Handler is the dashboard for one domain.
type Handler struct {
	auth   *auth.Service
	apps   *apps.Service
	logDir string
	origin string
	mux    *http.ServeMux
}

// NewHandler returns the dashboard served at https://domain. logDir holds
// the deploy logs the agent writes.
func NewHandler(service *auth.Service, appService *apps.Service, domain, logDir string) *Handler {
	h := &Handler{auth: service, apps: appService, logDir: logDir, origin: "https://" + domain, mux: http.NewServeMux()}

	h.mux.HandleFunc("GET /api/state", h.state)
	h.mux.HandleFunc("POST /api/setup/begin", h.beginSetup)
	h.mux.HandleFunc("POST /api/setup/finish", h.finishSetup)
	h.mux.HandleFunc("POST /api/login", h.login)
	h.mux.HandleFunc("POST /api/logout", h.logout)
	h.mux.HandleFunc("GET /api/events", h.requireSession(h.events))
	h.registerAppRoutes()
	h.mux.HandleFunc("/api/", h.notFound)
	h.mux.Handle("/", newWebApp())

	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	isAPI := strings.HasPrefix(r.URL.Path, "/api/")
	if isAPI {
		w.Header().Set("Cache-Control", "no-store")
	}

	if reason, err := h.checkSameOrigin(r); err != nil {
		h.auth.RecordRefusal(clientIP(r), r.URL.Path, reason)
		writeError(w, http.StatusForbidden, err)
		return
	}

	h.mux.ServeHTTP(w, r)
}

// checkSameOrigin refuses state-changing requests from other sites. Together
// with the SameSite=Strict cookie and the JSON-only API, it blocks cross-site
// request forgery: an HTML form can't send application/json.
func (h *Handler) checkSameOrigin(r *http.Request) (auth.RefusalReason, error) {
	isSafeMethod := r.Method == http.MethodGet || r.Method == http.MethodHead
	if isSafeMethod {
		return "", nil
	}

	origin := r.Header.Get("Origin")
	isCrossOrigin := origin != "" && origin != h.origin
	isCrossSite := r.Header.Get("Sec-Fetch-Site") == "cross-site"
	if isCrossOrigin || isCrossSite {
		return auth.RefusedCrossSite, errors.New("Cross-site requests are not allowed")
	}

	mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if mediaType != "application/json" {
		return auth.RefusedNotJSON, errors.New("Requests must be JSON")
	}

	return "", nil
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
	if !h.readJSON(w, r, &request) {
		return
	}

	enrolment, err := h.auth.BeginSetup(r.Context(), clientIP(r), request.Token, request.Password)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	qrCode, err := qrCodeDataURI(enrolment.URI)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, beginSetupResponse{Enrolment: enrolment, QRCode: qrCode})
}

// beginSetupResponse adds the QR code an authenticator app scans. It is
// rendered here, so the browser loads no QR library and nothing third-party.
type beginSetupResponse struct {
	auth.Enrolment
	QRCode string `json:"qrCode"`
}

// qrCodeDataURI encodes text as a PNG QR code in a data: URI, which the
// Content-Security-Policy allows for images.
func qrCodeDataURI(text string) (string, error) {
	code, err := qr.Encode(text, qr.M)
	if err != nil {
		return "", err
	}

	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(code.PNG()), nil
}

type finishSetupRequest struct {
	Token string `json:"token"`
	Code  string `json:"code"`
}

func (h *Handler) finishSetup(w http.ResponseWriter, r *http.Request) {
	var request finishSetupRequest
	if !h.readJSON(w, r, &request) {
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
	if !h.readJSON(w, r, &request) {
		return
	}

	result, err := h.auth.Login(r.Context(), clientIP(r), cookieValue(r, auth.DeviceCookie), request.Password, request.Code)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	// The session this browser had before logging in ends, so a token set
	// before authentication can't outlive it (OWASP ASVS v5.0.0-7.2.4).
	if err := h.auth.Logout(r.Context(), cookieValue(r, auth.SessionCookie)); err != nil {
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
	// Drops anything the browser cached or stored for the dashboard. Cookies
	// are left alone so the trusted-device cookie survives logout
	// (OWASP ASVS v5.0.0-14.3.1).
	w.Header().Set("Clear-Site-Data", `"cache", "storage"`)
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
			h.auth.RecordRefusal(clientIP(r), r.URL.Path, auth.RefusedNoSession)
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
		auth.ErrKeysUnavailable:    http.StatusServiceUnavailable,
		auth.ErrLoginLocked:        http.StatusTooManyRequests,
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

func (h *Handler) readJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		h.auth.RecordRefusal(clientIP(r), r.URL.Path, auth.RefusedMalformedBody)
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
