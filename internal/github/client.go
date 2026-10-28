// Package github is the agent's client for GitHub's API, the only code that
// talks to GitHub with the App's credentials. Every URL is built on fixed
// hosts from numeric ids and checked names, never from a URL found in a
// webhook or the database (ASVS v5.0.0-1.3.6).
package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	apiVersion = "2022-11-28"
	// maxResponse bounds what Tero reads from one answer.
	maxResponse = 10 << 20
	// maxPages bounds a paginated listing, at 100 items a page.
	maxPages = 20
	timeout  = 30 * time.Second
)

// ErrNotFound means GitHub answered 404: the App, installation, repository
// or delivery is gone or out of the App's reach.
var ErrNotFound = errors.New("not found on GitHub")

// codePattern is what a manifest conversion code looks like. The code
// arrives in a URL the admin's browser followed, so it is checked before it
// becomes part of a path.
var codePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

// Client calls GitHub's REST API.
type Client struct {
	http    *http.Client
	apiBase string
}

// New returns a client for api.github.com, or for the end-to-end suite's
// fake GitHub in e2e builds.
func New() *Client {
	apiBase := "https://api.github.com"
	if override, hasOverride := testAPIOverride(); hasOverride {
		apiBase = override
	}

	return &Client{http: &http.Client{Timeout: timeout}, apiBase: apiBase}
}

// Conversion is what GitHub returns once the admin created the App from
// Tero's manifest.
type Conversion struct {
	ID            int64   `json:"id"`
	Slug          string  `json:"slug"`
	Owner         Account `json:"owner"`
	PEM           string  `json:"pem"`
	WebhookSecret string  `json:"webhook_secret"`
}

// Account is a GitHub user or organization.
type Account struct {
	Login string `json:"login"`
	Type  string `json:"type"`
}

// IsOrganization reports whether the account is an organization.
func (a Account) IsOrganization() bool {
	return a.Type == "Organization"
}

// ConvertManifest trades the code GitHub sent the admin back with for the
// new App's id, private key and webhook secret. The code works once, within
// an hour, and needs no credential.
func (c *Client) ConvertManifest(ctx context.Context, code string) (Conversion, error) {
	if !codePattern.MatchString(code) {
		return Conversion{}, errors.New("the code from GitHub is malformed")
	}

	var conversion Conversion
	err := c.do(ctx, http.MethodPost, "/app-manifests/"+code+"/conversions", "", nil, &conversion)
	return conversion, err
}

// Installation is where the App is installed.
type Installation struct {
	ID          int64      `json:"id"`
	Account     Account    `json:"account"`
	SuspendedAt *time.Time `json:"suspended_at"`
}

// Installations lists the App's installations. A private App can only be
// installed on the account that owns it, so there is at most one.
func (c *Client) Installations(ctx context.Context, appToken string) ([]Installation, error) {
	var installations []Installation
	err := c.do(ctx, http.MethodGet, "/app/installations?per_page=100", appToken, nil, &installations)
	return installations, err
}

// InstallationToken is a token acting as the App's installation.
type InstallationToken struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Permission names a permission an installation token asks for.
type Permission string

const (
	ContentsRead Permission = "contents"
	MetadataRead Permission = "metadata"
)

// CreateInstallationToken mints a token for the installation, limited to
// repositoryIDs (all of the installation's when empty) and to reading the
// one permission given.
func (c *Client) CreateInstallationToken(ctx context.Context, appToken string, installationID int64, repositoryIDs []int64, permission Permission) (InstallationToken, error) {
	body := map[string]any{"permissions": map[string]string{string(permission): "read"}}
	if len(repositoryIDs) > 0 {
		body["repository_ids"] = repositoryIDs
	}

	path := "/app/installations/" + strconv.FormatInt(installationID, 10) + "/access_tokens"
	var token InstallationToken
	err := c.do(ctx, http.MethodPost, path, appToken, body, &token)
	return token, err
}

// RevokeInstallationToken ends a token before it expires.
func (c *Client) RevokeInstallationToken(ctx context.Context, token string) error {
	return c.do(ctx, http.MethodDelete, "/installation/token", token, nil, nil)
}

// Repository is a repository the installation can read.
type Repository struct {
	ID            int64   `json:"id"`
	Name          string  `json:"name"`
	Owner         Account `json:"owner"`
	IsPrivate     bool    `json:"private"`
	DefaultBranch string  `json:"default_branch"`
}

// InstallationRepositories lists every repository the installation token
// can read, up to maxPages pages of 100.
func (c *Client) InstallationRepositories(ctx context.Context, token string) ([]Repository, error) {
	var all []Repository
	for page := 1; page <= maxPages; page++ {
		var listing struct {
			TotalCount   int          `json:"total_count"`
			Repositories []Repository `json:"repositories"`
		}
		path := "/installation/repositories?per_page=100&page=" + strconv.Itoa(page)
		if err := c.do(ctx, http.MethodGet, path, token, nil, &listing); err != nil {
			return nil, err
		}

		all = append(all, listing.Repositories...)
		isLastPage := len(listing.Repositories) < 100 || len(all) >= listing.TotalCount
		if isLastPage {
			break
		}
	}

	return all, nil
}

// Delivery is one webhook delivery GitHub made for the App.
type Delivery struct {
	ID           int64     `json:"id"`
	GUID         string    `json:"guid"`
	Event        string    `json:"event"`
	StatusCode   int       `json:"status_code"`
	DeliveredAt  time.Time `json:"delivered_at"`
	IsRedelivery bool      `json:"redelivery"`
}

// IsFailed reports whether Tero didn't accept the delivery.
func (d Delivery) IsFailed() bool {
	return d.StatusCode < 200 || d.StatusCode >= 300
}

// RecentDeliveries lists the App's deliveries newest first, back to since,
// up to maxPages pages of 100.
func (c *Client) RecentDeliveries(ctx context.Context, appToken string, since time.Time) ([]Delivery, error) {
	var all []Delivery
	path := "/app/hook/deliveries?per_page=100"
	for range maxPages {
		var page []Delivery
		next, err := c.doPage(ctx, path, appToken, &page)
		if err != nil {
			return nil, err
		}

		for _, delivery := range page {
			if delivery.DeliveredAt.Before(since) {
				return all, nil
			}
			all = append(all, delivery)
		}
		if next == "" {
			break
		}
		path = next
	}

	return all, nil
}

// Redeliver asks GitHub to send a delivery again.
func (c *Client) Redeliver(ctx context.Context, appToken string, deliveryID int64) error {
	path := "/app/hook/deliveries/" + strconv.FormatInt(deliveryID, 10) + "/attempts"
	return c.do(ctx, http.MethodPost, path, appToken, nil, nil)
}

func (c *Client) do(ctx context.Context, method, path, token string, body, result any) error {
	_, err := c.request(ctx, method, path, token, body, result)
	return err
}

// doPage fetches one page and returns the path of the next, from GitHub's
// Link header, when it is on the same API host.
func (c *Client) doPage(ctx context.Context, path, token string, result any) (string, error) {
	header, err := c.request(ctx, http.MethodGet, path, token, nil, result)
	if err != nil {
		return "", err
	}

	return c.nextPagePath(header.Get("Link")), nil
}

func (c *Client) request(ctx context.Context, method, path, token string, body, result any) (http.Header, error) {
	var encoded io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		encoded = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.apiBase+path, encoded)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", apiVersion)
	req.Header.Set("User-Agent", "tero")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	response, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("couldn't reach GitHub: %w", err)
	}
	defer response.Body.Close()

	limited := io.LimitReader(response.Body, maxResponse)
	if response.StatusCode == http.StatusNotFound {
		return nil, ErrNotFound
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(limited, 512))
		return nil, fmt.Errorf("GitHub answered %d to %s %s: %s", response.StatusCode, method, routeOf(path), strings.TrimSpace(string(message)))
	}
	if result == nil {
		return response.Header, nil
	}

	return response.Header, json.NewDecoder(limited).Decode(result)
}

// nextPagePath reads rel="next" from a Link header, and keeps it only when
// it points at the API host Tero talks to.
func (c *Client) nextPagePath(link string) string {
	for _, part := range strings.Split(link, ",") {
		target, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		if !strings.Contains(params, `rel="next"`) {
			continue
		}

		next, err := url.Parse(strings.Trim(target, "<>"))
		base, baseErr := url.Parse(c.apiBase)
		if err != nil || baseErr != nil || next.Scheme != base.Scheme || next.Host != base.Host {
			return ""
		}
		return next.RequestURI()
	}

	return ""
}

// routeOf leaves the query out of an error message.
func routeOf(path string) string {
	route, _, _ := strings.Cut(path, "?")
	return route
}
