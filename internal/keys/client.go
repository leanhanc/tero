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
		return errorFor(answer.Error)
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

func (c *Client) BeginSetup(ctx context.Context, token, password string) (Enrolment, error) {
	var enrolment Enrolment
	err := c.call(ctx, VerbSetupBegin, setupBeginArgs{Token: token, Password: password}, &enrolment)
	return enrolment, err
}

func (c *Client) FinishSetup(ctx context.Context, ip, token, code string) error {
	return c.call(ctx, VerbSetupFinish, setupFinishArgs{IP: ip, Token: token, Code: code}, nil)
}

// CheckLogin returns nil when both factors pass.
func (c *Client) CheckLogin(ctx context.Context, ip, password, code string) error {
	return c.call(ctx, VerbLoginCheck, loginCheckArgs{IP: ip, Password: password, Code: code}, nil)
}

// ResetLogin deletes the admin and every setup link. Root only.
func (c *Client) ResetLogin(ctx context.Context) error {
	return c.call(ctx, VerbLoginReset, nil, nil)
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

func (c *Client) RecentEvents(ctx context.Context, limit int) ([]Event, error) {
	var events []Event
	err := c.call(ctx, VerbEventList, eventListArgs{Limit: limit}, &events)
	return events, err
}
