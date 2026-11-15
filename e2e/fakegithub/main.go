// Command fakegithub stands in for GitHub's API in the end-to-end suite, the
// way Pebble stands in for Let's Encrypt. e2e builds of Tero read its address
// from /opt/tero-e2e/github-api. Each manifest conversion creates a new App,
// owned by an account or an organization, installed on the suite's copy of
// leanhanc/tero. It checks every App token Tero signs, records the
// installation tokens it mints and revokes so the suite can check none
// outlives its fetch, lists the webhook deliveries the suite records, and
// redelivers them signed to Tero's webhook endpoint when asked.
package main

import (
	"bytes"
	"context"
	"crypto"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"flag"
	"log"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	firstAppID     = 4242
	firstAppSlug   = "tero-e2e"
	installationID = 77
	repositoryID   = 4242001
)

type account struct {
	Login string `json:"login"`
	Type  string `json:"type"`
}

// gitHubApp is the App the latest manifest conversion created.
type gitHubApp struct {
	id            int64
	slug          string
	owner         account
	key           *rsa.PrivateKey
	webhookSecret string
}

type installationState struct {
	IsSuspended bool `json:"isSuspended"`
	IsRemoved   bool `json:"isRemoved"`
}

// publicRepository is a public repository outside the App, which Tero polls.
type publicRepository struct {
	Owner  string `json:"owner"`
	Name   string `json:"name"`
	ID     int64  `json:"id"`
	IsGone bool   `json:"isGone"`
}

type delivery struct {
	ID           int64     `json:"id"`
	GUID         string    `json:"guid"`
	Event        string    `json:"event"`
	StatusCode   int       `json:"status_code"`
	DeliveredAt  time.Time `json:"delivered_at"`
	IsRedelivery bool      `json:"redelivery"`
	body         []byte
}

type mintedToken struct {
	Token         string            `json:"token"`
	RepositoryIDs []int64           `json:"repositoryIds"`
	Permissions   map[string]string `json:"permissions"`
}

type fakeGitHub struct {
	webhookURL string
	webhooks   *http.Client

	mutex              sync.Mutex
	app                *gitHubApp
	nextOwner          account
	installation       installationState
	publicRepositories []publicRepository
	deliveries         []delivery
	deliveryListings   int
	redeliveryRequests []int64
	minted             []mintedToken
	revoked            []string
}

func main() {
	listen := flag.String("listen", "127.0.0.1:9900", "address to listen on")
	webhookURL := flag.String("webhook", "", "Tero's webhook endpoint, where deliveries are redelivered")
	webhookAddress := flag.String("webhook-address", "127.0.0.1:443", "address the webhook endpoint's host is reached at")
	flag.Parse()

	fake := &fakeGitHub{
		webhookURL: *webhookURL,
		webhooks:   webhookClient(*webhookAddress),
		nextOwner:  account{Login: "leanhanc", Type: "User"},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /app-manifests/{code}/conversions", fake.convert)
	mux.HandleFunc("GET /app/installations", fake.withAppToken(fake.installations))
	mux.HandleFunc("POST /app/installations/{id}/access_tokens", fake.withAppToken(fake.accessToken))
	mux.HandleFunc("DELETE /installation/token", fake.revoke)
	mux.HandleFunc("GET /installation/repositories", fake.withInstallationToken(fake.repositories))
	mux.HandleFunc("GET /repos/{owner}/{repo}", fake.repository)
	mux.HandleFunc("GET /app/hook/deliveries", fake.withAppToken(fake.listDeliveries))
	mux.HandleFunc("POST /app/hook/deliveries/{id}/attempts", fake.withAppToken(fake.redeliver))
	mux.HandleFunc("GET /e2e/state", fake.state)
	mux.HandleFunc("POST /e2e/owner", fake.setNextOwner)
	mux.HandleFunc("POST /e2e/installation", fake.setInstallation)
	mux.HandleFunc("POST /e2e/public", fake.setPublicRepository)
	mux.HandleFunc("POST /e2e/deliveries", fake.recordDelivery)

	log.Fatal(http.ListenAndServe(*listen, mux))
}

// webhookClient reaches Tero's dashboard on this machine whatever its
// domain resolves to. The suite's certificates come from Pebble, so they
// aren't checked.
func webhookClient(address string) *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, address)
		},
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	return &http.Client{Transport: transport, Timeout: 30 * time.Second}
}

// convert creates a new App each time, with its own id, key and webhook
// secret, owned by the account the suite chose last.
func (f *fakeGitHub) convert(w http.ResponseWriter, r *http.Request) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"message": err.Error()})
		return
	}

	f.mutex.Lock()
	created := &gitHubApp{id: firstAppID, slug: firstAppSlug, owner: f.nextOwner, key: key, webhookSecret: randomHex(20)}
	if f.app != nil {
		created.id = f.app.id + 1
		created.slug = firstAppSlug + "-" + strconv.FormatInt(created.id-firstAppID+1, 10)
	}
	f.app = created
	f.installation = installationState{}
	f.mutex.Unlock()

	pemKey := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": created.id, "slug": created.slug, "owner": created.owner,
		"pem": string(pemKey), "webhook_secret": created.webhookSecret, "client_id": "Iv1.e2e", "client_secret": "unused",
	})
}

// withAppToken refuses a request whose App token isn't RS256-signed with
// the current App's key for its id, or has expired.
func (f *fakeGitHub) withAppToken(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !f.isValidAppToken(token) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "A JSON web token could not be decoded"})
			return
		}
		next(w, r)
	}
}

func (f *fakeGitHub) isValidAppToken(token string) bool {
	f.mutex.Lock()
	current := f.app
	f.mutex.Unlock()
	if current == nil {
		return false
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return false
	}

	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || rsa.VerifyPKCS1v15(&current.key.PublicKey, crypto.SHA256, digest[:], signature) != nil {
		return false
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	var claims struct {
		Issuer    string `json:"iss"`
		ExpiresAt int64  `json:"exp"`
	}
	if json.Unmarshal(payload, &claims) != nil {
		return false
	}

	isThisApp := claims.Issuer == strconv.FormatInt(current.id, 10)
	return isThisApp && time.Unix(claims.ExpiresAt, 0).After(time.Now())
}

// withInstallationToken refuses a request without a live installation
// token, as GitHub does.
func (f *fakeGitHub) withInstallationToken(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !f.hasLiveInstallationToken(r) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "Bad credentials"})
			return
		}
		next(w, r)
	}
}

func (f *fakeGitHub) hasLiveInstallationToken(r *http.Request) bool {
	token, hasToken := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !hasToken {
		return false
	}

	f.mutex.Lock()
	defer f.mutex.Unlock()
	isMinted := slices.ContainsFunc(f.minted, func(minted mintedToken) bool { return minted.Token == token })
	return isMinted && !slices.Contains(f.revoked, token)
}

func (f *fakeGitHub) installations(w http.ResponseWriter, r *http.Request) {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	if f.installation.IsRemoved {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	var suspendedAt any
	if f.installation.IsSuspended {
		suspendedAt = time.Now().UTC()
	}
	writeJSON(w, http.StatusOK, []map[string]any{
		{"id": installationID, "account": f.app.owner, "suspended_at": suspendedAt},
	})
}

func (f *fakeGitHub) accessToken(w http.ResponseWriter, r *http.Request) {
	f.mutex.Lock()
	installation := f.installation
	f.mutex.Unlock()
	if installation.IsRemoved || r.PathValue("id") != strconv.Itoa(installationID) {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
		return
	}
	if installation.IsSuspended {
		writeJSON(w, http.StatusForbidden, map[string]string{"message": "This installation has been suspended"})
		return
	}

	var request struct {
		RepositoryIDs []int64           `json:"repository_ids"`
		Permissions   map[string]string `json:"permissions"`
	}
	json.NewDecoder(r.Body).Decode(&request)

	token := mintedToken{Token: "ghs_" + randomHex(16), RepositoryIDs: request.RepositoryIDs, Permissions: request.Permissions}
	f.mutex.Lock()
	f.minted = append(f.minted, token)
	f.mutex.Unlock()

	writeJSON(w, http.StatusCreated, map[string]any{"token": token.Token, "expires_at": time.Now().Add(time.Hour).UTC()})
}

func (f *fakeGitHub) revoke(w http.ResponseWriter, r *http.Request) {
	token, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	f.mutex.Lock()
	f.revoked = append(f.revoked, token)
	f.mutex.Unlock()

	w.WriteHeader(http.StatusNoContent)
}

func (f *fakeGitHub) repositories(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"total_count": 1, "repositories": []any{privateRepository()}})
}

func privateRepository() map[string]any {
	return map[string]any{
		"id": repositoryID, "name": "tero", "owner": account{Login: "leanhanc", Type: "User"},
		"private": true, "default_branch": "main",
	}
}

// repository answers the agent's lookup of a repository's id: the private
// fixtures' repository with an installation token, as GitHub hides a private
// repository from anyone else, and the public repositories the suite added
// until they are gone.
func (f *fakeGitHub) repository(w http.ResponseWriter, r *http.Request) {
	owner, name := r.PathValue("owner"), r.PathValue("repo")
	if owner == "leanhanc" && name == "tero" {
		if !f.hasLiveInstallationToken(r) {
			writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
			return
		}
		writeJSON(w, http.StatusOK, privateRepository())
		return
	}

	public, isFound := f.findPublicRepository(owner, name)
	if !isFound || public.IsGone {
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": public.ID, "name": public.Name, "owner": account{Login: public.Owner, Type: "User"},
		"private": false, "default_branch": "main",
	})
}

func (f *fakeGitHub) findPublicRepository(owner, name string) (publicRepository, bool) {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	for _, public := range f.publicRepositories {
		if public.Owner == owner && public.Name == name {
			return public, true
		}
	}
	return publicRepository{}, false
}

// listDeliveries lists the App's deliveries newest first, as GitHub does.
func (f *fakeGitHub) listDeliveries(w http.ResponseWriter, r *http.Request) {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	f.deliveryListings++
	newestFirst := slices.Clone(f.deliveries)
	slices.Reverse(newestFirst)
	writeJSON(w, http.StatusOK, newestFirst)
}

// redeliver answers at once and sends the delivery again afterwards, as
// GitHub does, signed with the App's webhook secret. The attempt is listed
// as a redelivery with the status Tero answered.
func (f *fakeGitHub) redeliver(w http.ResponseWriter, r *http.Request) {
	deliveryID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)

	f.mutex.Lock()
	index := slices.IndexFunc(f.deliveries, func(d delivery) bool { return d.ID == deliveryID })
	if index < 0 {
		f.mutex.Unlock()
		writeJSON(w, http.StatusNotFound, map[string]string{"message": "Not Found"})
		return
	}
	original := f.deliveries[index]
	secret := f.app.webhookSecret
	f.redeliveryRequests = append(f.redeliveryRequests, deliveryID)
	f.mutex.Unlock()

	go f.send(original, secret)
	w.WriteHeader(http.StatusAccepted)
}

func (f *fakeGitHub) send(original delivery, secret string) {
	request, err := http.NewRequest(http.MethodPost, f.webhookURL, bytes.NewReader(original.body))
	if err != nil {
		log.Printf("redeliver %s: %v", original.GUID, err)
		return
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-GitHub-Event", original.Event)
	request.Header.Set("X-GitHub-Delivery", original.GUID)
	request.Header.Set("X-Hub-Signature-256", sign(secret, original.body))

	statusCode := 0
	response, err := f.webhooks.Do(request)
	if err != nil {
		log.Printf("redeliver %s: %v", original.GUID, err)
	} else {
		statusCode = response.StatusCode
		response.Body.Close()
	}

	f.mutex.Lock()
	defer f.mutex.Unlock()
	redelivery := original
	redelivery.ID = f.nextDeliveryID()
	redelivery.StatusCode = statusCode
	redelivery.DeliveredAt = time.Now().UTC()
	redelivery.IsRedelivery = true
	f.deliveries = append(f.deliveries, redelivery)
}

func (f *fakeGitHub) nextDeliveryID() int64 {
	return int64(len(f.deliveries) + 1)
}

func sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// recordDelivery adds a delivery GitHub made, such as one that failed while
// the server was down.
func (f *fakeGitHub) recordDelivery(w http.ResponseWriter, r *http.Request) {
	var recorded struct {
		GUID       string `json:"guid"`
		Event      string `json:"event"`
		Body       string `json:"body"`
		StatusCode int    `json:"statusCode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&recorded); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
		return
	}

	f.mutex.Lock()
	defer f.mutex.Unlock()
	added := delivery{
		ID: f.nextDeliveryID(), GUID: recorded.GUID, Event: recorded.Event, StatusCode: recorded.StatusCode,
		DeliveredAt: time.Now().UTC(), body: []byte(recorded.Body),
	}
	f.deliveries = append(f.deliveries, added)
	writeJSON(w, http.StatusCreated, added)
}

// setNextOwner picks the account the next conversion's App belongs to.
func (f *fakeGitHub) setNextOwner(w http.ResponseWriter, r *http.Request) {
	var owner account
	if err := json.NewDecoder(r.Body).Decode(&owner); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
		return
	}

	f.mutex.Lock()
	f.nextOwner = owner
	f.mutex.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

// setInstallation suspends or removes the App's installation, as the admin
// can on GitHub.
func (f *fakeGitHub) setInstallation(w http.ResponseWriter, r *http.Request) {
	var installation installationState
	if err := json.NewDecoder(r.Body).Decode(&installation); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
		return
	}

	f.mutex.Lock()
	f.installation = installation
	f.mutex.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

// setPublicRepository adds a public repository, or marks it gone.
func (f *fakeGitHub) setPublicRepository(w http.ResponseWriter, r *http.Request) {
	var public publicRepository
	if err := json.NewDecoder(r.Body).Decode(&public); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
		return
	}

	f.mutex.Lock()
	defer f.mutex.Unlock()
	f.publicRepositories = slices.DeleteFunc(f.publicRepositories, func(known publicRepository) bool {
		return known.Owner == public.Owner && known.Name == public.Name
	})
	f.publicRepositories = append(f.publicRepositories, public)
	w.WriteHeader(http.StatusNoContent)
}

// state is what the suite reads back: the current App and its webhook
// secret, to sign webhooks as GitHub would, the tokens minted and revoked,
// and the deliveries.
func (f *fakeGitHub) state(w http.ResponseWriter, r *http.Request) {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	state := map[string]any{
		"repositoryId": repositoryID, "minted": f.minted, "revoked": f.revoked,
		"deliveries": f.deliveries, "deliveryListings": f.deliveryListings, "redeliveryRequests": f.redeliveryRequests,
	}
	if f.app != nil {
		state["appId"] = f.app.id
		state["slug"] = f.app.slug
		state["webhookSecret"] = f.app.webhookSecret
	}
	writeJSON(w, http.StatusOK, state)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}

func randomHex(size int) string {
	buffer := make([]byte, size)
	rand.Read(buffer)
	return hex.EncodeToString(buffer)
}
