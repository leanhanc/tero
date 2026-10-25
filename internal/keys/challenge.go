package keys

import (
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
)

const (
	// challengeLifetime bounds how long a passkey ceremony may take.
	challengeLifetime = 2 * time.Minute
	// maxLoginChallenges caps the login challenges kept at once. Anyone can
	// ask for one, so a flood only ever costs this much memory: the oldest is
	// dropped first. maxLoginChallengesPerIP keeps one IP (or IPv6 /64) from
	// pushing out everyone else's, so a flood needs that many addresses per
	// eviction.
	maxLoginChallenges      = 1024
	maxLoginChallengesPerIP = 4
)

type challengeKind string

const (
	challengeLogin    challengeKind = "login"
	challengeRegister challengeKind = "register"
)

// challenge is a passkey ceremony in progress. It lives only in memory: a
// restart forgets it, which only fails the ceremonies in progress.
type challenge struct {
	kind challengeKind
	// sessionID is the session a registration belongs to; empty for logins.
	sessionID string
	// requester is the throttle key of the IP that asked for a login
	// challenge.
	requester string
	data      webauthn.SessionData
	expiresAt time.Time
}

// challenges holds the open passkey ceremonies by id. Each one works once.
type challenges struct {
	mu    sync.Mutex
	byID  map[string]challenge
	order []string
}

// add keeps a new challenge issued at now and returns its id. A session
// keeps at most one registration, and logins are capped at
// maxLoginChallenges.
func (c *challenges) add(entry challenge, now time.Time) (string, error) {
	id, err := newID()
	if err != nil {
		return "", err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.byID == nil {
		c.byID = map[string]challenge{}
	}
	c.dropExpired(now)
	if entry.kind == challengeRegister {
		c.dropRegistrationsFor(entry.sessionID)
	}
	if entry.kind == challengeLogin {
		c.dropOldestLogins(maxLoginChallengesPerIP-1, func(other challenge) bool { return other.requester == entry.requester })
		c.dropOldestLogins(maxLoginChallenges-1, func(challenge) bool { return true })
	}

	c.byID[id] = entry
	c.order = append(c.order, id)
	return id, nil
}

// take removes the challenge with id and returns it when it is of kind,
// belongs to sessionID and hasn't expired at now.
func (c *challenges) take(id string, kind challengeKind, sessionID string, now time.Time) (challenge, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, isFound := c.byID[id]
	if !isFound {
		return challenge{}, false
	}
	c.remove(id)

	isUsable := entry.kind == kind && entry.sessionID == sessionID && now.Before(entry.expiresAt)
	return entry, isUsable
}

func (c *challenges) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.byID = map[string]challenge{}
	c.order = nil
}

func (c *challenges) dropExpired(now time.Time) {
	for _, id := range append([]string(nil), c.order...) {
		if !now.Before(c.byID[id].expiresAt) {
			c.remove(id)
		}
	}
}

func (c *challenges) dropRegistrationsFor(sessionID string) {
	for _, id := range append([]string(nil), c.order...) {
		entry := c.byID[id]
		if entry.kind == challengeRegister && entry.sessionID == sessionID {
			c.remove(id)
		}
	}
}

// dropOldestLogins drops the oldest login challenges that match until at
// most limit are left.
func (c *challenges) dropOldestLogins(limit int, matches func(challenge) bool) {
	var logins []string
	for _, id := range c.order {
		entry := c.byID[id]
		if entry.kind == challengeLogin && matches(entry) {
			logins = append(logins, id)
		}
	}

	for len(logins) > limit {
		c.remove(logins[0])
		logins = logins[1:]
	}
}

func (c *challenges) remove(id string) {
	delete(c.byID, id)
	for index, ordered := range c.order {
		if ordered == id {
			c.order = append(c.order[:index], c.order[index+1:]...)
			return
		}
	}
}
