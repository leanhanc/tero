package keys

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/leanhanc/tero/internal/journal"
)

// EventType names a security event (OWASP ASVS v5.0.0-6.3.4, v5.0.0-16.3.1).
type EventType string

const (
	EventSetupLinkIssued    EventType = "setup_link_issued"
	EventSetupCompleted     EventType = "setup_completed"
	EventSetupCodeFailed    EventType = "setup_code_failed"
	EventLoginSucceeded     EventType = "login_succeeded"
	EventLoginFailed        EventType = "login_failed"
	EventCodeFailed         EventType = "totp_failed"
	EventCodeLocked         EventType = "totp_locked"
	EventLoginReset         EventType = "login_reset"
	EventKeyRotated         EventType = "key_rotated"
	EventCallersRefused     EventType = "callers_refused"
	EventEventsDropped      EventType = "events_dropped"
	EventRateLimited        EventType = "rate_limited"
	EventBreachCheckSkipped EventType = "breach_check_skipped"
	EventBreachReset        EventType = "breach_reset"
)

// Source is who recorded an event. The key service records its own events;
// a caller's events carry the caller, which the caller can't choose.
type Source string

const SourceKeys Source = "tero-keys"

var eventMessages = map[EventType]string{
	EventSetupLinkIssued:    "Dashboard setup link issued",
	EventSetupCompleted:     "Dashboard login set up",
	EventSetupCodeFailed:    "Dashboard setup failed: wrong TOTP code",
	EventLoginSucceeded:     "Dashboard login succeeded",
	EventLoginFailed:        "Dashboard login failed",
	EventCodeFailed:         "Dashboard login with the right password and a wrong TOTP code",
	EventCodeLocked:         "Dashboard login locked for 24 hours after too many wrong TOTP codes",
	EventLoginReset:         "Dashboard login reset on the server",
	EventKeyRotated:         "Master key rotated",
	EventCallersRefused:     "Key service refused connections from unknown processes",
	EventEventsDropped:      "Security events dropped over the per-minute limit",
	EventRateLimited:        "Dashboard logins rate limited",
	EventBreachCheckSkipped: "Online breached-password check unavailable; the bundled list was used",
	EventBreachReset:        "Server reset with sudo tero assume-breach",
}

// warnings are written to the journal at warning priority.
var warnings = map[EventType]bool{
	EventSetupCodeFailed: true, EventLoginFailed: true, EventCodeFailed: true, EventCodeLocked: true,
	EventLoginReset: true, EventCallersRefused: true, EventEventsDropped: true, EventRateLimited: true,
	EventBreachReset: true,
}

// appendable lists the events each caller may append. Everything else is
// recorded by the key service itself, from what it saw happen.
var appendable = map[Caller]map[EventType]bool{
	CallerDashboard: {EventRateLimited: true, EventBreachCheckSkipped: true},
	CallerRoot:      {EventBreachReset: true},
}

// Event is a recorded security event. It never holds a password, TOTP code,
// TOTP secret or token: only what happened, when, from where and who
// recorded it.
type Event struct {
	At     time.Time `json:"at"`
	Type   EventType `json:"type"`
	Source Source    `json:"source"`
	IP     string    `json:"ip,omitempty"`
	Count  int       `json:"count,omitempty"`
}

// JournalWriter sends events to the system journal.
type JournalWriter func(priority int, message string, fields map[string]string) error

const (
	// appendLimit is how many events a caller may append per minute.
	appendLimit = 60
	// maxEvents bounds the table; the oldest go first, except keptEvents.
	maxEvents = 100_000
	// loginCheckLimit is how many logins the dashboard may have checked per
	// minute, whoever they are for. It bounds how fast a compromised
	// dashboard can guess passwords and fill the log. The dashboard's own
	// throttle stops other browsers after 10 failures for the account, so
	// an attack from outside can't use it up and lock out the admin's
	// trusted browser.
	loginCheckLimit = 30
	// eventRetention is how long events are kept.
	eventRetention = 365 * 24 * time.Hour
	// maxIPLength fits any IPv6 address with a zone.
	maxIPLength = 64
)

// record stores an event and writes it to the journal from this unit, whose
// trusted _SYSTEMD_UNIT field marks the genuine copy. A journal failure
// doesn't fail the request: the database copy is the record.
func (s *Service) record(ctx context.Context, event Event) error {
	if event.Count == 0 {
		event.Count = 1
	}
	if len(event.IP) > maxIPLength {
		event.IP = event.IP[:maxIPLength]
	}

	_, err := s.db.ExecContext(ctx,
		"INSERT INTO security_events (at, type, source, ip, count) VALUES (?, ?, ?, ?, ?)",
		event.At.Unix(), string(event.Type), string(event.Source), event.IP, event.Count)
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
	if warnings[event.Type] {
		priority = journal.PriorityWarning
	}

	fields := map[string]string{"TERO_EVENT": string(event.Type), "TERO_EVENT_SOURCE": string(event.Source)}
	message := eventMessages[event.Type]
	if event.IP != "" {
		fields["TERO_SOURCE_IP"] = event.IP
		message += " from " + event.IP
	}
	if event.Count > 1 {
		fields["TERO_EVENT_COUNT"] = strconv.Itoa(event.Count)
		message += " (" + strconv.Itoa(event.Count) + " times)"
	}

	s.journal(priority, message, fields)
}

// recordOwn records an event the key service saw itself.
func (s *Service) recordOwn(ctx context.Context, eventType EventType, ip string) error {
	return s.record(ctx, Event{At: s.now(), Type: eventType, Source: SourceKeys, IP: ip})
}

// AppendEvent records an event a caller reports, within its per-minute
// allowance. Past the allowance the event is only counted, and the count is
// recorded as one events_dropped event when the minute ends.
func (s *Service) AppendEvent(ctx context.Context, caller Caller, eventType EventType, ip string) error {
	if !appendable[caller][eventType] {
		return ErrRefused
	}
	if !s.limits.allowAppend(caller, s.now()) {
		return nil
	}

	return s.record(ctx, Event{At: s.now(), Type: eventType, Source: Source(caller), IP: ip})
}

// RecentEvents returns the latest events, newest first.
func (s *Service) RecentEvents(ctx context.Context, limit int) ([]Event, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT at, type, source, ip, count FROM security_events ORDER BY id DESC LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := []Event{}
	for rows.Next() {
		var unix int64
		var event Event
		if err := rows.Scan(&unix, &event.Type, &event.Source, &event.IP, &event.Count); err != nil {
			return nil, err
		}
		event.At = time.Unix(unix, 0).UTC()
		events = append(events, event)
	}

	return events, rows.Err()
}

// limits counts appends per caller and refused connections per minute.
type limits struct {
	mu      sync.Mutex
	minute  int64
	appends map[Caller]int
	checks  int
	dropped int
	refused int
	// checksRefused counts login checks over loginCheckLimit.
	checksRefused int
}

func (l *limits) roll(now time.Time) {
	minute := now.Unix() / 60
	if minute != l.minute {
		l.minute = minute
		l.appends = map[Caller]int{}
		l.checks = 0
	}
}

func (l *limits) allowAppend(caller Caller, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.roll(now)
	l.appends[caller]++
	if l.appends[caller] > appendLimit {
		l.dropped++
		return false
	}

	return true
}

func (l *limits) allowCheck(now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.roll(now)
	l.checks++
	if l.checks > loginCheckLimit {
		l.checksRefused++
		return false
	}

	return true
}

func (l *limits) countRefused() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.refused++
}

// take returns and resets the dropped, refused and refused-check counts.
func (l *limits) take() (dropped, refused, checksRefused int) {
	l.mu.Lock()
	defer l.mu.Unlock()

	dropped, refused, checksRefused = l.dropped, l.refused, l.checksRefused
	l.dropped, l.refused, l.checksRefused = 0, 0, 0
	return dropped, refused, checksRefused
}

// FlushCounts records the dropped events, refused connections and refused
// login checks counted since the last flush as one event each. The server
// calls it every minute.
func (s *Service) FlushCounts(ctx context.Context) error {
	dropped, refused, checksRefused := s.limits.take()
	for _, count := range []struct {
		eventType EventType
		count     int
	}{
		{EventEventsDropped, dropped},
		{EventCallersRefused, refused},
		{EventRateLimited, checksRefused},
	} {
		if count.count == 0 {
			continue
		}
		event := Event{At: s.now(), Type: count.eventType, Source: SourceKeys, Count: count.count}
		if err := s.record(ctx, event); err != nil {
			return err
		}
	}

	return nil
}
