package auth

import (
	"context"
	"time"

	"github.com/leanhanc/tero/internal/journal"
)

// EventType names a security event. Every login path produces one
// (OWASP ASVS v5.0.0-6.3.4).
type EventType string

const (
	EventSetupLinkIssued    EventType = "setup_link_issued"
	EventSetupCompleted     EventType = "setup_completed"
	EventLoginSucceeded     EventType = "login_succeeded"
	EventLoginFailed        EventType = "login_failed"
	EventSetupCodeFailed    EventType = "setup_code_failed"
	EventRateLimited        EventType = "rate_limited"
	EventLoginReset         EventType = "login_reset"
	EventBreachCheckSkipped EventType = "breach_check_skipped"
)

var eventMessages = map[EventType]string{
	EventSetupLinkIssued:    "Dashboard setup link issued",
	EventSetupCompleted:     "Dashboard login set up",
	EventLoginSucceeded:     "Dashboard login succeeded",
	EventLoginFailed:        "Dashboard login failed",
	EventSetupCodeFailed:    "Dashboard setup failed: wrong TOTP code",
	EventRateLimited:        "Dashboard logins rate limited",
	EventLoginReset:         "Dashboard login reset with sudo tero reset-login",
	EventBreachCheckSkipped: "Online breached-password check unavailable; the bundled list was used",
}

// Event is a recorded security event. It never holds a password, TOTP code,
// TOTP secret or token: only what happened, when and from where.
type Event struct {
	At     time.Time `json:"at"`
	Type   EventType `json:"type"`
	IP     string    `json:"ip,omitempty"`
	Detail string    `json:"detail,omitempty"`
}

// JournalWriter sends events to the system journal.
type JournalWriter func(priority int, message string, fields map[string]string) error

// record stores the event and writes it to the journal. A journal failure
// does not fail the request: the database copy is the one the dashboard shows.
func (s *Service) record(ctx context.Context, event Event) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO security_events (at, type, ip, detail) VALUES (?, ?, ?, ?)",
		event.At.Unix(), string(event.Type), event.IP, event.Detail)
	if err != nil {
		return err
	}

	s.writeJournal(event)
	return nil
}

func (s *Service) writeJournal(event Event) {
	if s.journal == nil {
		return
	}

	priority := journal.PriorityInfo
	isWarning := event.Type == EventLoginFailed || event.Type == EventSetupCodeFailed ||
		event.Type == EventRateLimited || event.Type == EventLoginReset
	if isWarning {
		priority = journal.PriorityWarning
	}

	fields := map[string]string{"TERO_EVENT": string(event.Type)}
	if event.IP != "" {
		fields["TERO_SOURCE_IP"] = event.IP
	}

	message := eventMessages[event.Type]
	if event.IP != "" {
		message += " from " + event.IP
	}

	s.journal(priority, message, fields)
}

// RecentEvents returns the latest events, newest first.
func (s *Service) RecentEvents(ctx context.Context, limit int) ([]Event, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT at, type, ip, detail FROM security_events ORDER BY id DESC LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := []Event{}
	for rows.Next() {
		var unix int64
		var event Event
		if err := rows.Scan(&unix, &event.Type, &event.IP, &event.Detail); err != nil {
			return nil, err
		}
		event.At = time.Unix(unix, 0).UTC()
		events = append(events, event)
	}

	return events, rows.Err()
}
