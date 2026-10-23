package keys

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
)

// The protocol is one request and one answer per connection, each a 4-byte
// big-endian length followed by that many bytes of JSON.

// maxMessage bounds a request or an answer.
const maxMessage = 64 << 10

// Verbs, and which caller may send each. No verb returns the master key, the
// password hash or a stored TOTP secret.
const (
	VerbStatus      = "status"
	VerbSetupIssue  = "setup.issue"
	VerbSetupBegin  = "setup.begin"
	VerbSetupFinish = "setup.finish"
	VerbLoginCheck  = "login.check"
	VerbLoginReset  = "login.reset"
	VerbKeyRotate   = "key.rotate"
	VerbEventAppend = "events.append"
	VerbEventList   = "events.list"
)

var verbs = map[Caller]map[string]bool{
	CallerDashboard: {
		VerbStatus: true, VerbSetupBegin: true, VerbSetupFinish: true, VerbLoginCheck: true,
		VerbEventAppend: true, VerbEventList: true,
	},
	CallerRoot: {
		VerbStatus: true, VerbSetupIssue: true, VerbLoginReset: true, VerbKeyRotate: true,
		VerbEventAppend: true, VerbEventList: true,
	},
}

type request struct {
	Verb string          `json:"verb"`
	Args json.RawMessage `json:"args,omitempty"`
}

type response struct {
	Error  string          `json:"error,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
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
}

func codeFor(err error) string {
	for code, known := range errorCodes {
		if errors.Is(err, known) {
			return code
		}
	}

	return "internal"
}

func errorFor(code string) error {
	if known, isKnown := errorCodes[code]; isKnown {
		return known
	}

	return fmt.Errorf("the key service failed (%s)", code)
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

// Request arguments and results.
type (
	setupBeginArgs struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	setupFinishArgs struct {
		IP    string `json:"ip"`
		Token string `json:"token"`
		Code  string `json:"code"`
	}
	loginCheckArgs struct {
		IP       string `json:"ip"`
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	eventAppendArgs struct {
		Type EventType `json:"type"`
		IP   string    `json:"ip"`
	}
	eventListArgs struct {
		Limit int `json:"limit"`
	}
	setupIssueResult struct {
		Token string `json:"token"`
	}
	rotateResult struct {
		InUse []int `json:"inUse"`
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
		var args setupBeginArgs
		if err := decodeArgs(req, &args); err != nil {
			return nil, err
		}
		return s.BeginSetup(ctx, args.Token, args.Password)
	case VerbSetupFinish:
		var args setupFinishArgs
		if err := decodeArgs(req, &args); err != nil {
			return nil, err
		}
		return struct{}{}, s.FinishSetup(ctx, args.IP, args.Token, args.Code)
	case VerbLoginCheck:
		var args loginCheckArgs
		if err := decodeArgs(req, &args); err != nil {
			return nil, err
		}
		return struct{}{}, s.CheckLogin(ctx, args.IP, args.Password, args.Code)
	case VerbLoginReset:
		return struct{}{}, s.ResetLogin(ctx)
	case VerbKeyRotate:
		inUse, err := s.RotateKey(ctx)
		return rotateResult{InUse: inUse}, err
	case VerbEventAppend:
		var args eventAppendArgs
		if err := decodeArgs(req, &args); err != nil {
			return nil, err
		}
		return struct{}{}, s.AppendEvent(ctx, caller, args.Type, args.IP)
	case VerbEventList:
		var args eventListArgs
		if err := decodeArgs(req, &args); err != nil {
			return nil, err
		}
		events, err := s.RecentEvents(ctx, min(max(args.Limit, 1), maxListedEvents))
		if caller == CallerDashboard {
			hideWhichFactorFailed(events)
		}
		return events, err
	default:
		return nil, ErrRefused
	}
}

func decodeArgs(req request, args any) error {
	if err := json.Unmarshal(req.Args, args); err != nil {
		return ErrRefused
	}

	return nil
}

// hideWhichFactorFailed shows the dashboard a wrong code as a failed login.
// A compromised dashboard could otherwise guess passwords and read here which
// guess was right. Root and the journal see the real type.
func hideWhichFactorFailed(events []Event) {
	for i := range events {
		if events[i].Type == EventCodeFailed {
			events[i].Type = EventLoginFailed
		}
	}
}
