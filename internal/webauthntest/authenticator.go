// Package webauthntest is a software passkey authenticator for tests: it
// answers the options the key service issues the way a browser and a
// platform authenticator would, with ES256 keys and attestation "none".
package webauthntest

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"

	"github.com/fxamacker/cbor/v2"
)

// Authenticator flags (WebAuthn §6.1).
const (
	flagUserPresent  = 0x01
	flagUserVerified = 0x04
	flagAttested     = 0x40
)

// Authenticator holds passkeys for one origin. Its fields let tests answer
// the way a broken or hostile authenticator would.
type Authenticator struct {
	// Origin is what the browser puts in client data, such as
	// https://dash.example.com.
	Origin string
	// SkipUserVerification answers without the user-verified flag.
	SkipUserVerification bool
	// RPID overrides the relying party ID the answer is made for.
	RPID string
	// Counter is the next signature counter; it goes up by one per answer.
	Counter uint32

	credentials []credential
}

type credential struct {
	id         []byte
	key        *ecdsa.PrivateKey
	rpID       string
	userHandle []byte
}

// creationOptions and requestOptions are the parts of the WebAuthn options
// an authenticator reads.
type creationOptions struct {
	Challenge string `json:"challenge"`
	RP        struct {
		ID string `json:"id"`
	} `json:"rp"`
	User struct {
		ID string `json:"id"`
	} `json:"user"`
}

type requestOptions struct {
	Challenge string `json:"challenge"`
	RPID      string `json:"rpId"`
}

var encoding = base64.RawURLEncoding

// Register makes a passkey for the creation options and returns the
// browser's answer as JSON.
func (a *Authenticator) Register(options json.RawMessage) (json.RawMessage, error) {
	id := make([]byte, 32)
	rand.Read(id)
	return a.register(options, id)
}

// RegisterWithID is Register for a passkey whose id, in base64url, is
// already taken, as an authenticator that ignores the exclusion list makes.
func (a *Authenticator) RegisterWithID(options json.RawMessage, id string) (json.RawMessage, error) {
	decoded, err := decode(id)
	if err != nil {
		return nil, err
	}
	return a.register(options, decoded)
}

func (a *Authenticator) register(options json.RawMessage, id []byte) (json.RawMessage, error) {
	var parsed creationOptions
	if err := json.Unmarshal(options, &parsed); err != nil {
		return nil, err
	}
	userHandle, err := decode(parsed.User.ID)
	if err != nil {
		return nil, err
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	a.credentials = append(a.credentials, credential{id: id, key: key, rpID: parsed.RP.ID, userHandle: userHandle})

	authData, err := a.attestedAuthData(a.rpID(parsed.RP.ID), id, key)
	if err != nil {
		return nil, err
	}
	attestation, err := cbor.Marshal(map[string]any{"fmt": "none", "attStmt": map[string]any{}, "authData": authData})
	if err != nil {
		return nil, err
	}

	clientData := a.clientData("webauthn.create", parsed.Challenge)
	return json.Marshal(map[string]any{
		"id":    encoding.EncodeToString(id),
		"rawId": encoding.EncodeToString(id),
		"type":  "public-key",
		"response": map[string]any{
			"clientDataJSON":    encoding.EncodeToString(clientData),
			"attestationObject": encoding.EncodeToString(attestation),
		},
	})
}

// Login signs the request options with the newest passkey made for their
// relying party and returns the browser's answer as JSON.
func (a *Authenticator) Login(options json.RawMessage) (json.RawMessage, error) {
	var parsed requestOptions
	if err := json.Unmarshal(options, &parsed); err != nil {
		return nil, err
	}

	passkey, err := a.passkeyFor(parsed.RPID)
	if err != nil {
		return nil, err
	}
	authData := a.authData(a.rpID(parsed.RPID), a.flags())
	clientData := a.clientData("webauthn.get", parsed.Challenge)
	clientDataHash := sha256.Sum256(clientData)
	digest := sha256.Sum256(append(append([]byte(nil), authData...), clientDataHash[:]...))
	signature, err := ecdsa.SignASN1(rand.Reader, passkey.key, digest[:])
	if err != nil {
		return nil, err
	}

	return json.Marshal(map[string]any{
		"id":    encoding.EncodeToString(passkey.id),
		"rawId": encoding.EncodeToString(passkey.id),
		"type":  "public-key",
		"response": map[string]any{
			"clientDataJSON":    encoding.EncodeToString(clientData),
			"authenticatorData": encoding.EncodeToString(authData),
			"signature":         encoding.EncodeToString(signature),
			"userHandle":        encoding.EncodeToString(passkey.userHandle),
		},
	})
}

func (a *Authenticator) passkeyFor(rpID string) (credential, error) {
	for i := len(a.credentials) - 1; i >= 0; i-- {
		if a.credentials[i].rpID == rpID {
			return a.credentials[i], nil
		}
	}

	return credential{}, errors.New("no passkey for this relying party")
}

func (a *Authenticator) rpID(fromOptions string) string {
	if a.RPID != "" {
		return a.RPID
	}

	return fromOptions
}

func (a *Authenticator) flags() byte {
	if a.SkipUserVerification {
		return flagUserPresent
	}

	return flagUserPresent | flagUserVerified
}

// authData is the authenticator data without attested credential data.
func (a *Authenticator) authData(rpID string, flags byte) []byte {
	rpIDHash := sha256.Sum256([]byte(rpID))
	data := append(rpIDHash[:], flags)
	data = binary.BigEndian.AppendUint32(data, a.Counter)
	a.Counter++

	return data
}

// attestedAuthData adds the new credential's id and COSE public key.
func (a *Authenticator) attestedAuthData(rpID string, id []byte, key *ecdsa.PrivateKey) ([]byte, error) {
	publicKey, err := key.PublicKey.Bytes()
	if err != nil {
		return nil, err
	}
	// Uncompressed point: 0x04 || x || y.
	coseKey, err := cbor.Marshal(map[int]any{1: 2, 3: -7, -1: 1, -2: publicKey[1:33], -3: publicKey[33:65]})
	if err != nil {
		return nil, err
	}

	data := a.authData(rpID, a.flags()|flagAttested)
	data = append(data, make([]byte, 16)...)
	data = binary.BigEndian.AppendUint16(data, uint16(len(id)))
	data = append(data, id...)

	return append(data, coseKey...), nil
}

func (a *Authenticator) clientData(ceremony, challenge string) []byte {
	encoded, _ := json.Marshal(map[string]any{"type": ceremony, "challenge": challenge, "origin": a.Origin, "crossOrigin": false})
	return encoded
}

// decode reads base64url with or without padding, as option fields come.
func decode(value string) ([]byte, error) {
	if decoded, err := encoding.DecodeString(value); err == nil {
		return decoded, nil
	}

	return base64.URLEncoding.DecodeString(value)
}
