package keys

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"time"
)

// Client sends requests to the key service.
type Client struct {
	do func(ctx context.Context, req request) (response, error)
}

// dialTimeout covers a tero-keys start: systemd queues the connection while
// the service comes up.
const dialTimeout = 10 * time.Second

// NewClient talks to the key service on its socket.
func NewClient(socketPath string) *Client {
	return &Client{do: func(ctx context.Context, req request) (response, error) {
		dialer := net.Dialer{Timeout: dialTimeout}
		conn, err := dialer.DialContext(ctx, "unix", socketPath)
		if err != nil {
			return response{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
		defer conn.Close()

		deadline := time.Now().Add(requestTimeout + idleTimeout)
		if ctxDeadline, hasDeadline := ctx.Deadline(); hasDeadline && ctxDeadline.Before(deadline) {
			deadline = ctxDeadline
		}
		conn.SetDeadline(deadline)

		if err := writeMessage(conn, req); err != nil {
			return response{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
		}
		var answer response
		if err := readMessage(conn, &answer); err != nil {
			return response{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
		}

		return answer, nil
	}}
}

// NewLocalClient calls service in this process as caller, through the same
// checks and encoding as the socket. Tests use it in place of a running key
// service.
func NewLocalClient(service *Service, caller Caller) *Client {
	return &Client{do: func(ctx context.Context, req request) (response, error) {
		encoded, err := json.Marshal(req)
		if err != nil {
			return response{}, err
		}
		var decoded request
		if err := json.Unmarshal(encoded, &decoded); err != nil {
			return response{}, err
		}

		return service.handle(ctx, caller, decoded), nil
	}}
}

func (c *Client) call(ctx context.Context, verb string, args, result any) error {
	req := request{Verb: verb}
	if args != nil {
		encoded, err := json.Marshal(args)
		if err != nil {
			return err
		}
		req.Args = encoded
	}

	answer, err := c.do(ctx, req)
	if err != nil {
		return err
	}
	if answer.Error != "" {
		return errorFor(answer)
	}
	if result == nil {
		return nil
	}

	return json.Unmarshal(answer.Result, result)
}

func (c *Client) Status(ctx context.Context) (Status, error) {
	var status Status
	err := c.call(ctx, VerbStatus, nil, &status)
	return status, err
}

// IssueSetupLink returns a new setup token. Root only.
func (c *Client) IssueSetupLink(ctx context.Context) (string, error) {
	var result setupIssueResult
	err := c.call(ctx, VerbSetupIssue, nil, &result)
	return result.Token, err
}

func (c *Client) BeginSetup(ctx context.Context, ip, token, password string) (Enrolment, error) {
	var enrolment Enrolment
	err := c.call(ctx, VerbSetupBegin, setupBeginArgs{IP: ip, Token: token, Password: password}, &enrolment)
	return enrolment, err
}

// FinishSetup creates the admin and returns the new session.
func (c *Client) FinishSetup(ctx context.Context, browser Browser, token, code string) (LoginResult, error) {
	var result LoginResult
	err := c.call(ctx, VerbSetupFinish, setupFinishArgs{Browser: browser, Token: token, Code: code}, &result)
	return result, err
}

// LoginPassword starts a session when both factors pass.
func (c *Client) LoginPassword(ctx context.Context, browser Browser, password, code string) (LoginResult, error) {
	var result LoginResult
	err := c.call(ctx, VerbLoginPassword, loginPasswordArgs{Browser: browser, Password: password, Code: code}, &result)
	return result, err
}

func (c *Client) BeginPasskeyLogin(ctx context.Context, ip string) (PasskeyOptions, error) {
	var options PasskeyOptions
	err := c.call(ctx, VerbLoginPasskeyBegin, loginPasskeyBeginArgs{IP: ip}, &options)
	return options, err
}

// LoginPasskey starts a session when the passkey answer checks out.
func (c *Client) LoginPasskey(ctx context.Context, browser Browser, challengeID string, answer json.RawMessage) (LoginResult, error) {
	var result LoginResult
	err := c.call(ctx, VerbLoginPasskeyFinish, loginPasskeyArgs{Browser: browser, ChallengeID: challengeID, Answer: answer}, &result)
	return result, err
}

// ResetLogin deletes the admin, passkeys, setup links and every session,
// trusted browser and throttle. Root only.
func (c *Client) ResetLogin(ctx context.Context) error {
	return c.call(ctx, VerbLoginReset, nil, nil)
}

// CheckSession returns ErrSessionEnded unless session is live, and marks it
// used from ip.
func (c *Client) CheckSession(ctx context.Context, session, ip string) (SessionState, error) {
	var state SessionState
	err := c.call(ctx, VerbSessionCheck, sessionArgs{Session: session, IP: ip}, &state)
	return state, err
}

func (c *Client) EndSession(ctx context.Context, session string) error {
	return c.call(ctx, VerbSessionEnd, sessionArgs{Session: session}, nil)
}

func (c *Client) ListSessions(ctx context.Context, session string) ([]SessionInfo, error) {
	var sessions []SessionInfo
	err := c.call(ctx, VerbSessionList, sessionArgs{Session: session}, &sessions)
	return sessions, err
}

// RevokeSession ends the session with id, or every other session when
// isAllOthers is set, and returns how many ended.
func (c *Client) RevokeSession(ctx context.Context, session, ip, id string, isAllOthers bool) (int, error) {
	var result revokeResult
	err := c.call(ctx, VerbSessionRevoke, sessionRevokeArgs{Session: session, IP: ip, ID: id, IsAllOthers: isAllOthers}, &result)
	return result.Ended, err
}

func (c *Client) Confirm(ctx context.Context, session string, browser Browser, password, code string) error {
	return c.call(ctx, VerbAccountConfirm, confirmArgs{Session: session, Browser: browser, Password: password, Code: code}, nil)
}

func (c *Client) ChangePassword(ctx context.Context, session string, browser Browser, currentPassword, code, newPassword string, isEndingOthers bool) error {
	args := passwordChangeArgs{
		Session: session, Browser: browser, CurrentPassword: currentPassword, Code: code,
		NewPassword: newPassword, IsEndingOthers: isEndingOthers,
	}
	return c.call(ctx, VerbPasswordChange, args, nil)
}

func (c *Client) BeginPasskeyRegistration(ctx context.Context, session string) (PasskeyOptions, error) {
	var options PasskeyOptions
	err := c.call(ctx, VerbPasskeyRegisterBegin, sessionArgs{Session: session}, &options)
	return options, err
}

func (c *Client) FinishPasskeyRegistration(ctx context.Context, session, ip, challengeID, name string, answer json.RawMessage, isEndingOthers bool) (PasskeyInfo, error) {
	var info PasskeyInfo
	args := passkeyRegisterArgs{
		Session: session, IP: ip, ChallengeID: challengeID, Name: name, Answer: answer, IsEndingOthers: isEndingOthers,
	}
	err := c.call(ctx, VerbPasskeyRegister, args, &info)
	return info, err
}

func (c *Client) ListPasskeys(ctx context.Context, session string) ([]PasskeyInfo, error) {
	var passkeys []PasskeyInfo
	err := c.call(ctx, VerbPasskeyList, sessionArgs{Session: session}, &passkeys)
	return passkeys, err
}

func (c *Client) RenamePasskey(ctx context.Context, session, id, name string) error {
	return c.call(ctx, VerbPasskeyRename, passkeyRenameArgs{Session: session, ID: id, Name: name}, nil)
}

func (c *Client) RemovePasskey(ctx context.Context, session, ip, id string, isEndingOthers bool) error {
	return c.call(ctx, VerbPasskeyRemove, passkeyRemoveArgs{Session: session, IP: ip, ID: id, IsEndingOthers: isEndingOthers}, nil)
}

// RotateKey re-seals everything with the newest loaded key and returns the
// versions still in use. Root only.
func (c *Client) RotateKey(ctx context.Context) ([]int, error) {
	var result rotateResult
	err := c.call(ctx, VerbKeyRotate, nil, &result)
	return result.InUse, err
}

func (c *Client) AppendEvent(ctx context.Context, eventType EventType, ip string) error {
	return c.call(ctx, VerbEventAppend, eventAppendArgs{Type: eventType, IP: ip}, nil)
}

// RecentEvents lists the latest events, newest first. The dashboard passes
// its caller's session; root passes none.
func (c *Client) RecentEvents(ctx context.Context, session string, limit int) ([]Event, error) {
	var events []Event
	err := c.call(ctx, VerbEventList, eventListArgs{Session: session, Limit: limit}, &events)
	return events, err
}
