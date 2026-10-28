// Command fakegithub stands in for GitHub's API in the end-to-end suite, the
// way Pebble stands in for Let's Encrypt. e2e builds of Tero read its address
// from /opt/tero-e2e/github-api. It creates one App, installed on the
// suite's copy of leanhanc/tero, checks every App token Tero signs, and
// records the installation tokens it mints and revokes so the suite can
// check none outlives its fetch.
package main

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"flag"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	appID          = 4242
	appSlug        = "tero-e2e"
	installationID = 77
	repositoryID   = 4242001
)

type fakeGitHub struct {
	key           *rsa.PrivateKey
	webhookSecret string

	mutex   sync.Mutex
	minted  []mintedToken
	revoked []string
}

type mintedToken struct {
	Token         string            `json:"token"`
	RepositoryIDs []int64           `json:"repositoryIds"`
	Permissions   map[string]string `json:"permissions"`
}

func main() {
	listen := flag.String("listen", "127.0.0.1:9900", "address to listen on")
	flag.Parse()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		log.Fatal(err)
	}
	fake := &fakeGitHub{key: key, webhookSecret: randomHex(20)}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /app-manifests/{code}/conversions", fake.convert)
	mux.HandleFunc("GET /app/installations", fake.withAppToken(fake.installations))
	mux.HandleFunc("POST /app/installations/{id}/access_tokens", fake.withAppToken(fake.accessToken))
	mux.HandleFunc("DELETE /installation/token", fake.revoke)
	mux.HandleFunc("GET /installation/repositories", fake.repositories)
	mux.HandleFunc("GET /app/hook/deliveries", fake.withAppToken(fake.deliveries))
	mux.HandleFunc("POST /app/hook/deliveries/{id}/attempts", fake.withAppToken(fake.redeliver))
	mux.HandleFunc("GET /e2e/state", fake.state)

	log.Fatal(http.ListenAndServe(*listen, mux))
}

func (f *fakeGitHub) convert(w http.ResponseWriter, r *http.Request) {
	pemKey := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(f.key)})
	writeJSON(w, http.StatusCreated, map[string]any{
		"id": appID, "slug": appSlug, "owner": map[string]string{"login": "leanhanc", "type": "User"},
		"pem": string(pemKey), "webhook_secret": f.webhookSecret, "client_id": "Iv1.e2e", "client_secret": "unused",
	})
}

// withAppToken refuses a request whose App token isn't RS256-signed with
// the App's key for the App's id, or has expired.
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
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return false
	}

	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || rsa.VerifyPKCS1v15(&f.key.PublicKey, crypto.SHA256, digest[:], signature) != nil {
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

	return claims.Issuer == "4242" && time.Unix(claims.ExpiresAt, 0).After(time.Now())
}

func (f *fakeGitHub) installations(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, []map[string]any{
		{"id": installationID, "account": map[string]string{"login": "leanhanc", "type": "User"}, "suspended_at": nil},
	})
}

func (f *fakeGitHub) accessToken(w http.ResponseWriter, r *http.Request) {
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
	writeJSON(w, http.StatusOK, map[string]any{
		"total_count": 1,
		"repositories": []map[string]any{{
			"id": repositoryID, "name": "tero", "owner": map[string]string{"login": "leanhanc", "type": "User"},
			"private": true, "default_branch": "main",
		}},
	})
}

func (f *fakeGitHub) deliveries(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, []any{})
}

func (f *fakeGitHub) redeliver(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusAccepted)
}

// state is what the suite reads back: the webhook secret, to sign webhooks
// as GitHub would, and the tokens minted and revoked.
func (f *fakeGitHub) state(w http.ResponseWriter, r *http.Request) {
	f.mutex.Lock()
	defer f.mutex.Unlock()

	writeJSON(w, http.StatusOK, map[string]any{
		"webhookSecret": f.webhookSecret, "repositoryId": repositoryID, "minted": f.minted, "revoked": f.revoked,
	})
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
