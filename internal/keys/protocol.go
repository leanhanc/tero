package keys

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"
)

// The protocol is one request and one answer per connection, each a 4-byte
// big-endian length followed by that many bytes of JSON.

// maxMessage bounds a request or an answer. It fits a webhook body of
// MaxWebhookBody, base64-encoded in JSON, and so anything else the dashboard
// accepts.
const maxMessage = 2 << 20

// Verbs, and which caller may send each. No verb returns the master key, the
// password hash, a stored TOTP secret or a token hash; only the three that
// start a session return its token. Every dashboard verb that reads or
// changes the account takes the caller's session token.
const (
	VerbStatus               = "status"
	VerbSetupIssue           = "setup.issue"
	VerbSetupBegin           = "setup.begin"
	VerbSetupFinish          = "setup.finish"
	VerbLoginPassword        = "login.password"
	VerbLoginPasskeyBegin    = "login.passkey.begin"
	VerbLoginPasskeyFinish   = "login.passkey.finish"
	VerbLoginReset           = "login.reset"
	VerbSessionCheck         = "session.check"
	VerbSessionEnd           = "session.end"
	VerbSessionList          = "session.list"
	VerbSessionRevoke        = "session.revoke"
	VerbAccountConfirm       = "account.confirm"
	VerbPasswordChange       = "password.change"
	VerbPasskeyRegisterBegin = "passkey.register.begin"
	VerbPasskeyRegister      = "passkey.register.finish"
	VerbPasskeyList          = "passkey.list"
	VerbPasskeyRename        = "passkey.rename"
	VerbPasskeyRemove        = "passkey.remove"
	VerbKeyRotate            = "key.rotate"
	VerbEventAppend          = "events.append"
	VerbEventList            = "events.list"

	VerbGitHubConnectBegin   = "github.connect.begin"
	VerbGitHubConnectApprove = "github.connect.approve"
	VerbGitHubConnect        = "github.connect"
	VerbGitHubDisconnect     = "github.disconnect"
	VerbGitHubForget         = "github.forget"
	VerbGitHubStatus         = "github.status"
	VerbGitHubJWT            = "github.jwt"
	VerbGitHubWebhookVerify  = "github.webhook.verify"
)

var verbs = map[Caller]map[string]bool{
	CallerDashboard: {
		VerbStatus: true, VerbSetupBegin: true, VerbSetupFinish: true,
		VerbLoginPassword: true, VerbLoginPasskeyBegin: true, VerbLoginPasskeyFinish: true,
		VerbSessionCheck: true, VerbSessionEnd: true, VerbSessionList: true, VerbSessionRevoke: true,
		VerbAccountConfirm: true, VerbPasswordChange: true,
		VerbPasskeyRegisterBegin: true, VerbPasskeyRegister: true, VerbPasskeyList: true, VerbPasskeyRename: true,
		VerbPasskeyRemove: true,
		VerbEventAppend:   true, VerbEventList: true,
		VerbGitHubConnectBegin: true, VerbGitHubConnectApprove: true, VerbGitHubDisconnect: true,
		VerbGitHubStatus: true, VerbGitHubWebhookVerify: true,
	},
	// Only the agent talks to GitHub with the App's credentials, so only it
	// stores the App and gets App tokens.
	CallerAgent: {
		VerbGitHubConnect: true, VerbGitHubStatus: true, VerbGitHubJWT: true,
	},
	CallerRoot: {
		VerbStatus: true, VerbSetupIssue: true, VerbLoginReset: true, VerbKeyRotate: true,
		VerbEventAppend: true, VerbEventList: true,
		VerbGitHubForget: true, VerbGitHubStatus: true,
	},
}

type request struct {
	Verb string          `json:"verb"`
	Args json.RawMessage `json:"args,omitempty"`
}

type response struct {
	Error string `json:"error,omitempty"`
	// RetryAfter is in seconds, for a rate_limited error.
	RetryAfter int             `json:"retryAfter,omitempty"`
	Result     json.RawMessage `json:"result,omitempty"`
}

// errorCodes are the errors that cross the socket. Anything else is answered
// as "internal" and logged, so internals don't reach the caller.
var errorCodes = map[string]error{
	"refused":         ErrRefused,
	"invalid_link":    ErrInvalidLink,
	"already_set_up":  ErrAlreadySetUp,
	"not_set_up":      ErrNotSetUp,
	"invalid_code":    ErrInvalidCode,
	"wrong_login":     ErrWrongLogin,
	"locked":          ErrLocked,
	"key_not_loaded":  ErrKeyNotLoaded,
	"hashing_is_busy": ErrHashingIsBusy,
	"session_ended":   ErrSessionEnded,
	"not_confirmed":   ErrNotConfirmed,
	"passkey_limit":   ErrPasskeyLimit,
	"invalid_name":    ErrInvalidName,
	"invalid_passkey": ErrInvalidPasskey,
	"not_found":       ErrNotFound,

	"github_connected":     ErrGitHubConnected,
	"github_not_connected": ErrGitHubNotConnected,
	"invalid_connection":   ErrInvalidConnection,
	"invalid_app_key":      ErrInvalidAppKey,
}

const rateLimitedCode = "rate_limited"

func codeFor(err error) string {
	for code, known := range errorCodes {
		if errors.Is(err, known) {
			return code
		}
	}

	return "internal"
}

func errorFor(answer response) error {
	code := answer.Error
	if code == rateLimitedCode {
		return RateLimitedError{RetryAfter: time.Duration(answer.RetryAfter) * time.Second}
	}
	if known, isKnown := errorCodes[code]; isKnown {
		return known
	}

	return fmt.Errorf("the key service failed (%s). `journalctl -u tero-keys` says why", code)
}

func writeMessage(w io.Writer, value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(encoded) > maxMessage {
		return errors.New("message too large")
	}

	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(encoded)))
	if _, err := w.Write(length[:]); err != nil {
		return err
	}
	_, err = w.Write(encoded)
	return err
}

func readMessage(r io.Reader, value any) error {
	var length [4]byte
	if _, err := io.ReadFull(r, length[:]); err != nil {
		return err
	}

	size := binary.BigEndian.Uint32(length[:])
	if size > maxMessage {
		return errors.New("message too large")
	}

	encoded := make([]byte, size)
	if _, err := io.ReadFull(r, encoded); err != nil {
		return err
	}

	return json.Unmarshal(encoded, value)
}

// Request arguments and results. Session is the caller's session token.
type (
	setupBeginArgs struct {
		IP       string `json:"ip"`
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	setupFinishArgs struct {
		Browser Browser `json:"browser"`
		Token   string  `json:"token"`
		Code    string  `json:"code"`
	}
	loginPasswordArgs struct {
		Browser  Browser `json:"browser"`
		Password string  `json:"password"`
		Code     string  `json:"code"`
	}
	loginPasskeyBeginArgs struct {
		IP string `json:"ip"`
	}
	loginPasskeyArgs struct {
		Browser     Browser         `json:"browser"`
		ChallengeID string          `json:"challengeId"`
		Answer      json.RawMessage `json:"answer"`
	}
	sessionArgs struct {
		Session string `json:"session"`
		IP      string `json:"ip"`
	}
	sessionRevokeArgs struct {
		Session     string `json:"session"`
		IP          string `json:"ip"`
		ID          string `json:"id"`
		IsAllOthers bool   `json:"isAllOthers"`
	}
	confirmArgs struct {
		Session  string  `json:"session"`
		Browser  Browser `json:"browser"`
		Password string  `json:"password"`
		Code     string  `json:"code"`
	}
	passwordChangeArgs struct {
		Session         string  `json:"session"`
		Browser         Browser `json:"browser"`
		CurrentPassword string  `json:"currentPassword"`
		Code            string  `json:"code"`
		NewPassword     string  `json:"newPassword"`
		IsEndingOthers  bool    `json:"isEndingOthers"`
	}
	passkeyRegisterArgs struct {
		Session        string          `json:"session"`
		IP             string          `json:"ip"`
		ChallengeID    string          `json:"challengeId"`
		Name           string          `json:"name"`
		Answer         json.RawMessage `json:"answer"`
		IsEndingOthers bool            `json:"isEndingOthers"`
	}
	passkeyRenameArgs struct {
		Session string `json:"session"`
		ID      string `json:"id"`
		Name    string `json:"name"`
	}
	passkeyRemoveArgs struct {
		Session        string `json:"session"`
		IP             string `json:"ip"`
		ID             string `json:"id"`
		IsEndingOthers bool   `json:"isEndingOthers"`
	}
	eventAppendArgs struct {
		Type EventType `json:"type"`
		IP   string    `json:"ip"`
	}
	eventListArgs struct {
		Session string `json:"session"`
		Limit   int    `json:"limit"`
	}
	gitHubApproveArgs struct {
		Session string `json:"session"`
		State   string `json:"state"`
	}
	webhookVerifyArgs struct {
		Body      []byte `json:"body"`
		Signature string `json:"signature"`
		IP        string `json:"ip"`
	}
	gitHubStateResult struct {
		State string `json:"state"`
	}
	webhookVerifyResult struct {
		IsValid bool `json:"isValid"`
	}
	setupIssueResult struct {
		Token string `json:"token"`
	}
	rotateResult struct {
		InUse []int `json:"inUse"`
	}
	revokeResult struct {
		Ended int `json:"ended"`
	}
)

// maxListedEvents bounds events.list.
const maxListedEvents = 1000

// handle runs one request for caller and returns the answer.
func (s *Service) handle(ctx context.Context, caller Caller, req request) response {
	if !verbs[caller][req.Verb] {
		slog.Warn("request refused", "caller", caller, "verb", req.Verb)
		return response{Error: codeFor(ErrRefused)}
	}

	result, err := s.dispatch(ctx, caller, req)
	var limited RateLimitedError
	if errors.As(err, &limited) {
		return response{Error: rateLimitedCode, RetryAfter: int(limited.RetryAfter.Seconds())}
	}
	if err != nil {
		code := codeFor(err)
		if code == "internal" {
			slog.Error("request failed", "caller", caller, "verb", req.Verb, "error", err)
		}
		return response{Error: code}
	}

	encoded, err := json.Marshal(result)
	if err != nil {
		return response{Error: "internal"}
	}

	return response{Result: encoded}
}

func (s *Service) dispatch(ctx context.Context, caller Caller, req request) (any, error) {
	switch req.Verb {
	case VerbStatus:
		return s.Status(ctx)
	case VerbSetupIssue:
		token, err := s.IssueSetupLink(ctx)
		return setupIssueResult{Token: token}, err
	case VerbSetupBegin:
		return withArgs(req, func(args setupBeginArgs) (any, error) {
			return s.BeginSetup(ctx, args.IP, args.Token, args.Password)
		})
	case VerbSetupFinish:
		return withArgs(req, func(args setupFinishArgs) (any, error) {
			return s.FinishSetup(ctx, args.Browser, args.Token, args.Code)
		})
	case VerbLoginPassword:
		return withArgs(req, func(args loginPasswordArgs) (any, error) {
			return s.LoginPassword(ctx, args.Browser, args.Password, args.Code)
		})
	case VerbLoginPasskeyBegin:
		return withArgs(req, func(args loginPasskeyBeginArgs) (any, error) {
			return s.BeginPasskeyLogin(ctx, args.IP)
		})
	case VerbLoginPasskeyFinish:
		return withArgs(req, func(args loginPasskeyArgs) (any, error) {
			return s.LoginPasskey(ctx, args.Browser, args.ChallengeID, args.Answer)
		})
	case VerbLoginReset:
		return struct{}{}, s.ResetLogin(ctx)
	case VerbSessionCheck:
		return withArgs(req, func(args sessionArgs) (any, error) {
			return s.CheckSession(ctx, args.Session, args.IP)
		})
	case VerbSessionEnd:
		return withArgs(req, func(args sessionArgs) (any, error) {
			return struct{}{}, s.EndSession(ctx, args.Session)
		})
	case VerbSessionList:
		return withArgs(req, func(args sessionArgs) (any, error) {
			return s.ListSessions(ctx, args.Session)
		})
	case VerbSessionRevoke:
		return withArgs(req, func(args sessionRevokeArgs) (any, error) {
			ended, err := s.RevokeSession(ctx, args.Session, args.IP, args.ID, args.IsAllOthers)
			return revokeResult{Ended: ended}, err
		})
	case VerbAccountConfirm:
		return withArgs(req, func(args confirmArgs) (any, error) {
			return struct{}{}, s.Confirm(ctx, args.Session, args.Browser, args.Password, args.Code)
		})
	case VerbPasswordChange:
		return withArgs(req, func(args passwordChangeArgs) (any, error) {
			err := s.ChangePassword(ctx, args.Session, args.Browser, args.CurrentPassword, args.Code, args.NewPassword, args.IsEndingOthers)
			return struct{}{}, err
		})
	case VerbPasskeyRegisterBegin:
		return withArgs(req, func(args sessionArgs) (any, error) {
			return s.BeginPasskeyRegistration(ctx, args.Session)
		})
	case VerbPasskeyRegister:
		return withArgs(req, func(args passkeyRegisterArgs) (any, error) {
			return s.FinishPasskeyRegistration(ctx, args.Session, args.IP, args.ChallengeID, args.Name, args.Answer, args.IsEndingOthers)
		})
	case VerbPasskeyList:
		return withArgs(req, func(args sessionArgs) (any, error) {
			return s.ListPasskeys(ctx, args.Session)
		})
	case VerbPasskeyRename:
		return withArgs(req, func(args passkeyRenameArgs) (any, error) {
			return struct{}{}, s.RenamePasskey(ctx, args.Session, args.ID, args.Name)
		})
	case VerbPasskeyRemove:
		return withArgs(req, func(args passkeyRemoveArgs) (any, error) {
			return struct{}{}, s.RemovePasskey(ctx, args.Session, args.IP, args.ID, args.IsEndingOthers)
		})
	case VerbKeyRotate:
		inUse, err := s.RotateKey(ctx)
		return rotateResult{InUse: inUse}, err
	case VerbEventAppend:
		return withArgs(req, func(args eventAppendArgs) (any, error) {
			return struct{}{}, s.AppendEvent(ctx, caller, args.Type, args.IP)
		})
	case VerbEventList:
		return withArgs(req, func(args eventListArgs) (any, error) {
			return s.listEventsFor(ctx, caller, args)
		})
	case VerbGitHubConnectBegin:
		return withArgs(req, func(args sessionArgs) (any, error) {
			state, err := s.BeginGitHubConnection(ctx, args.Session)
			return gitHubStateResult{State: state}, err
		})
	case VerbGitHubConnectApprove:
		return withArgs(req, func(args gitHubApproveArgs) (any, error) {
			return struct{}{}, s.ApproveGitHubConnection(ctx, args.Session, args.State)
		})
	case VerbGitHubConnect:
		return withArgs(req, func(args GitHubAppSecrets) (any, error) {
			return struct{}{}, s.ConnectGitHub(ctx, args)
		})
	case VerbGitHubDisconnect:
		return withArgs(req, func(args sessionArgs) (any, error) {
			return struct{}{}, s.DisconnectGitHub(ctx, args.Session)
		})
	case VerbGitHubForget:
		return struct{}{}, s.ForgetGitHub(ctx)
	case VerbGitHubStatus:
		return s.GitHubStatus(ctx)
	case VerbGitHubJWT:
		return s.SignAppToken(ctx)
	case VerbGitHubWebhookVerify:
		return withArgs(req, func(args webhookVerifyArgs) (any, error) {
			isValid, err := s.VerifyWebhook(ctx, args.Body, args.Signature, args.IP)
			return webhookVerifyResult{IsValid: isValid}, err
		})
	default:
		return nil, ErrRefused
	}
}

func withArgs[Args any](req request, run func(Args) (any, error)) (any, error) {
	var args Args
	if err := json.Unmarshal(req.Args, &args); err != nil {
		return nil, ErrRefused
	}

	return run(args)
}

// listEventsFor lists events. The dashboard needs a live session for it, and
// sees a wrong code as a failed login or confirmation: a compromised
// dashboard could otherwise guess passwords and read here which guess was
// right. Root and the journal see the real type.
func (s *Service) listEventsFor(ctx context.Context, caller Caller, args eventListArgs) ([]Event, error) {
	if caller == CallerDashboard {
		if _, err := s.liveSession(ctx, args.Session); err != nil {
			return nil, err
		}
	}

	events, err := s.RecentEvents(ctx, min(max(args.Limit, 1), maxListedEvents))
	if err != nil {
		return nil, err
	}
	if caller == CallerDashboard {
		hideWhichFactorFailed(events)
	}

	return events, nil
}

var shownToDashboard = map[EventType]EventType{
	EventCodeFailed:        EventLoginFailed,
	EventConfirmCodeFailed: EventConfirmFailed,
}

func hideWhichFactorFailed(events []Event) {
	for i := range events {
		if shown, isHidden := shownToDashboard[events[i].Type]; isHidden {
			events[i].Type = shown
		}
	}
}
