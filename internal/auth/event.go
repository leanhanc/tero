package auth

import (
	"context"
	"fmt"

	"github.com/leanhanc/tero/internal/journal"
	"github.com/leanhanc/tero/internal/keys"
)

// Event is a recorded security event; the key service keeps them.
type Event = keys.Event

// JournalWriter sends entries to the system journal.
type JournalWriter func(priority int, message string, fields map[string]string) error

// record has the key service store an event only the dashboard can see. If
// the key service can't take it, the event goes to the journal marked as not
// stored, so it isn't lost, and the request carries on.
func (s *Service) record(ctx context.Context, eventType keys.EventType, ip string) {
	err := s.keys.AppendEvent(ctx, eventType, ip)
	if err == nil || s.journal == nil {
		return
	}

	fields := map[string]string{"TERO_EVENT": string(eventType), "TERO_EVENT_UNSTORED": "1"}
	if ip != "" {
		fields["TERO_SOURCE_IP"] = ip
	}
	s.journal(journal.PriorityWarning, fmt.Sprintf("Security event %s could not be stored: %v", eventType, err), fields)
}

// RecentEvents returns the latest events, newest first, for the caller's
// session.
func (s *Service) RecentEvents(ctx context.Context, session string, limit int) ([]Event, error) {
	events, err := s.keys.RecentEvents(ctx, session, limit)
	return events, fromKeys(err)
}

// RefusalReason says why the dashboard refused a request.
type RefusalReason string

const (
	RefusedNoSession     RefusalReason = "no_session"
	RefusedCrossSite     RefusalReason = "cross_site"
	RefusedNotJSON       RefusalReason = "not_json"
	RefusedMalformedBody RefusalReason = "malformed_body"
)

// maxLoggedPathLength bounds the path in a refusal entry, so a huge request
// line can't make the entry too big for the journal socket.
const maxLoggedPathLength = 256

// RecordRefusal writes a refused request to the journal (OWASP ASVS
// v5.0.0-16.3.2, v5.0.0-16.3.3). Anyone on the internet can cause these, so
// they go only to the journal, which rate-limits and rotates on its own, and
// never to the events table the dashboard home shows.
func (s *Service) RecordRefusal(ip, path string, reason RefusalReason) {
	if s.journal == nil {
		return
	}

	loggedPath := path
	isPathTooLong := len(loggedPath) > maxLoggedPathLength
	if isPathTooLong {
		loggedPath = loggedPath[:maxLoggedPathLength]
	}

	fields := map[string]string{
		"TERO_EVENT":     "request_refused",
		"TERO_REASON":    string(reason),
		"TERO_PATH":      loggedPath,
		"TERO_SOURCE_IP": ip,
	}
	message := fmt.Sprintf("Dashboard request to %q refused (%s) from %s", loggedPath, reason, ip)

	s.journal(journal.PriorityWarning, message, fields)
}
