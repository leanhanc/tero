package dashboard

import (
	"encoding/json"
	"net/http"

	"github.com/leanhanc/tero/internal/auth"
)

// registerAccountRoutes adds the settings page's API: the password, the
// session list and passkeys. Each call passes the session cookie on to the
// key service, which acts only for a live session, and for a confirmed one
// where the action needs it.
func (h *Handler) registerAccountRoutes() {
	h.mux.HandleFunc("GET /api/account", h.requireSession(h.account))
	h.mux.HandleFunc("POST /api/account/confirm", h.requireSession(h.confirm))
	h.mux.HandleFunc("POST /api/account/password", h.requireSession(h.changePassword))
	h.mux.HandleFunc("POST /api/sessions/end", h.requireSession(h.endSessions))
	h.mux.HandleFunc("POST /api/passkeys/register/begin", h.requireSession(h.beginPasskeyRegistration))
	h.mux.HandleFunc("POST /api/passkeys/register/finish", h.requireSession(h.finishPasskeyRegistration))
	h.mux.HandleFunc("POST /api/passkeys/rename", h.requireSession(h.renamePasskey))
	h.mux.HandleFunc("POST /api/passkeys/remove", h.requireSession(h.removePasskey))
}

type accountResponse struct {
	IsConfirmed bool               `json:"isConfirmed"`
	Sessions    []auth.SessionInfo `json:"sessions"`
	Passkeys    []auth.PasskeyInfo `json:"passkeys"`
}

func (h *Handler) account(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	token := sessionToken(r)

	sessions, err := h.auth.Sessions(ctx, token)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	passkeys, err := h.auth.Passkeys(ctx, token)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, accountResponse{IsConfirmed: currentSession(r).IsConfirmed, Sessions: sessions, Passkeys: passkeys})
}

type confirmRequest struct {
	Password string `json:"password"`
	Code     string `json:"code"`
}

func (h *Handler) confirm(w http.ResponseWriter, r *http.Request) {
	var request confirmRequest
	if !h.readJSON(w, r, &request) {
		return
	}

	err := h.auth.Confirm(r.Context(), sessionToken(r), browserOf(r), request.Password, request.Code)
	h.answer(w, r, err)
}

type changePasswordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	Code            string `json:"code"`
	NewPassword     string `json:"newPassword"`
	IsEndingOthers  bool   `json:"isEndingOthers"`
}

func (h *Handler) changePassword(w http.ResponseWriter, r *http.Request) {
	var request changePasswordRequest
	if !h.readJSON(w, r, &request) {
		return
	}

	err := h.auth.ChangePassword(r.Context(), sessionToken(r), browserOf(r),
		request.CurrentPassword, request.Code, request.NewPassword, request.IsEndingOthers)
	h.answer(w, r, err)
}

type endSessionsRequest struct {
	ID          string `json:"id"`
	IsAllOthers bool   `json:"isAllOthers"`
}

func (h *Handler) endSessions(w http.ResponseWriter, r *http.Request) {
	var request endSessionsRequest
	if !h.readJSON(w, r, &request) {
		return
	}

	err := h.auth.EndSessions(r.Context(), sessionToken(r), clientIP(r), request.ID, request.IsAllOthers)
	h.answer(w, r, err)
}

func (h *Handler) beginPasskeyRegistration(w http.ResponseWriter, r *http.Request) {
	var request struct{}
	if !h.readJSON(w, r, &request) {
		return
	}

	options, err := h.auth.BeginPasskeyRegistration(r.Context(), sessionToken(r))
	if err != nil {
		h.fail(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, options)
}

type passkeyRegistrationRequest struct {
	ChallengeID    string          `json:"challengeId"`
	Name           string          `json:"name"`
	Credential     json.RawMessage `json:"credential"`
	IsEndingOthers bool            `json:"isEndingOthers"`
}

func (h *Handler) finishPasskeyRegistration(w http.ResponseWriter, r *http.Request) {
	var request passkeyRegistrationRequest
	if !h.readJSON(w, r, &request) {
		return
	}

	passkey, err := h.auth.FinishPasskeyRegistration(r.Context(), sessionToken(r), clientIP(r),
		request.ChallengeID, request.Name, request.Credential, request.IsEndingOthers)
	if err != nil {
		h.fail(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, passkey)
}

type renamePasskeyRequest struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (h *Handler) renamePasskey(w http.ResponseWriter, r *http.Request) {
	var request renamePasskeyRequest
	if !h.readJSON(w, r, &request) {
		return
	}

	err := h.auth.RenamePasskey(r.Context(), sessionToken(r), request.ID, request.Name)
	h.answer(w, r, err)
}

type removePasskeyRequest struct {
	ID             string `json:"id"`
	IsEndingOthers bool   `json:"isEndingOthers"`
}

func (h *Handler) removePasskey(w http.ResponseWriter, r *http.Request) {
	var request removePasskeyRequest
	if !h.readJSON(w, r, &request) {
		return
	}

	err := h.auth.RemovePasskey(r.Context(), sessionToken(r), clientIP(r), request.ID, request.IsEndingOthers)
	h.answer(w, r, err)
}

// answer is 204 on success, or the error's response.
func (h *Handler) answer(w http.ResponseWriter, r *http.Request, err error) {
	if err != nil {
		h.fail(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
