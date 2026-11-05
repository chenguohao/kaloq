package server

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

func b64(v []byte) string { return base64.RawURLEncoding.EncodeToString(v) }
func testPasskeyRP(t *testing.T) *webauthn.WebAuthn {
	t.Helper()
	s := &Server{Origin: "https://staff.example.com"}
	rp, err := s.passkeyRP()
	if err != nil {
		t.Fatal(err)
	}
	return rp
}
func testPasskeyOwner() passkeyUser {
	return passkeyUser{User: User{ID: "6ee7e4c2-84af-4dfe-965a-cd9720349b14", LoginName: "employee", DisplayName: "Employee"}}
}
func passkeyClientData(kind, challenge, origin string) []byte {
	b, _ := json.Marshal(map[string]any{"type": kind, "challenge": challenge, "origin": origin, "crossOrigin": false})
	return b
}

// A software authenticator used solely for protocol verification. Its private
// key never enters the server registration data and is not a physical-device test.
func registrationFixture(t *testing.T, sd *webauthn.SessionData, origin, rp string, flags byte) ([]byte, *ecdsa.PrivateKey, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id := make([]byte, 32)
	if _, err = rand.Read(id); err != nil {
		t.Fatal(err)
	}
	cose, err := cbor.Marshal(map[int]any{1: 2, 3: -7, -1: 1, -2: key.X.FillBytes(make([]byte, 32)), -3: key.Y.FillBytes(make([]byte, 32))})
	if err != nil {
		t.Fatal(err)
	}
	rpHash := sha256.Sum256([]byte(rp))
	auth := append([]byte{}, rpHash[:]...)
	auth = append(auth, flags)
	auth = append(auth, 0, 0, 0, 0)
	auth = append(auth, make([]byte, 16)...)
	auth = append(auth, 0, byte(len(id)))
	auth = append(auth, id...)
	auth = append(auth, cose...)
	att, err := cbor.Marshal(map[string]any{"fmt": "none", "authData": auth, "attStmt": map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]any{"id": b64(id), "rawId": b64(id), "type": "public-key", "response": map[string]any{"clientDataJSON": b64(passkeyClientData("webauthn.create", sd.Challenge, origin)), "attestationObject": b64(att), "transports": []string{"internal"}}, "clientExtensionResults": map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	return raw, key, id
}
func assertionFixture(t *testing.T, sd *webauthn.SessionData, key *ecdsa.PrivateKey, id []byte, origin, rp string, flags byte, counter uint32) []byte {
	t.Helper()
	rpHash := sha256.Sum256([]byte(rp))
	auth := append([]byte{}, rpHash[:]...)
	auth = append(auth, flags)
	auth = binary.BigEndian.AppendUint32(auth, counter)
	client := passkeyClientData("webauthn.get", sd.Challenge, origin)
	clientHash := sha256.Sum256(client)
	signed := append(append([]byte{}, auth...), clientHash[:]...)
	digest := sha256.Sum256(signed)
	sig, err := ecdsa.SignASN1(rand.Reader, key, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]any{"id": b64(id), "rawId": b64(id), "type": "public-key", "response": map[string]any{"clientDataJSON": b64(client), "authenticatorData": b64(auth), "signature": b64(sig), "userHandle": b64(sd.UserID)}, "clientExtensionResults": map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestPasskeyPolicyAndRegistration(t *testing.T) {
	rp := testPasskeyRP(t)
	owner := testPasskeyOwner()
	options, sd, err := rp.BeginRegistration(owner)
	if err != nil {
		t.Fatal(err)
	}
	if options.Response.AuthenticatorSelection.UserVerification != protocol.VerificationRequired || sd.Expires.IsZero() {
		t.Fatal("must require UV and expiry")
	}
	_, other, err := rp.BeginRegistration(owner)
	if err != nil {
		t.Fatal(err)
	}
	if sd.Challenge == other.Challenge {
		t.Fatal("reused challenge")
	}
	cases := []struct {
		name, origin, rpid string
		flags              byte
		mutate             func(*webauthn.SessionData)
		valid              bool
	}{
		{"valid", "https://staff.example.com", "staff.example.com", 0x45, nil, true},
		{"wrong_origin", "https://evil.example.com", "staff.example.com", 0x45, nil, false},
		{"wrong_rpid", "https://staff.example.com", "evil.example.com", 0x45, nil, false},
		{"no_user_verification", "https://staff.example.com", "staff.example.com", 0x41, nil, false},
		{"no_user_presence", "https://staff.example.com", "staff.example.com", 0x44, nil, false},
		{"expired", "https://staff.example.com", "staff.example.com", 0x45, func(s *webauthn.SessionData) { s.Expires = time.Now().Add(-time.Second) }, false},
		{"wrong_owner", "https://staff.example.com", "staff.example.com", 0x45, func(s *webauthn.SessionData) { s.UserID = []byte("different employee") }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, _, _ := registrationFixture(t, sd, tc.origin, tc.rpid, tc.flags)
			session := *sd
			if tc.mutate != nil {
				tc.mutate(&session)
			}
			parsed, e := protocol.ParseCredentialCreationResponseBytes(raw)
			if e != nil {
				t.Fatal(e)
			}
			_, e = rp.CreateCredential(owner, session, parsed)
			if (e == nil) != tc.valid {
				t.Fatalf("valid=%v error=%v", tc.valid, e)
			}
		})
	}
}

func TestPasskeyAssertionSecurity(t *testing.T) {
	rp := testPasskeyRP(t)
	owner := testPasskeyOwner()
	_, sd, err := rp.BeginRegistration(owner)
	if err != nil {
		t.Fatal(err)
	}
	raw, key, id := registrationFixture(t, sd, "https://staff.example.com", "staff.example.com", 0x45)
	parsed, err := protocol.ParseCredentialCreationResponseBytes(raw)
	if err != nil {
		t.Fatal(err)
	}
	cred, err := rp.CreateCredential(owner, *sd, parsed)
	if err != nil {
		t.Fatal(err)
	}
	owner.Credentials = []webauthn.Credential{*cred}
	cases := []struct {
		name, origin, rpid string
		flags              byte
		mutate             func(*webauthn.SessionData)
		wrongKey, valid    bool
	}{
		{"valid", "https://staff.example.com", "staff.example.com", 5, nil, false, true},
		{"wrong_origin", "https://evil.example.com", "staff.example.com", 5, nil, false, false},
		{"wrong_rpid", "https://staff.example.com", "evil.example.com", 5, nil, false, false},
		{"no_uv", "https://staff.example.com", "staff.example.com", 1, nil, false, false},
		{"no_up", "https://staff.example.com", "staff.example.com", 4, nil, false, false},
		{"wrong_challenge", "https://staff.example.com", "staff.example.com", 5, func(s *webauthn.SessionData) { s.Challenge = b64(make([]byte, 32)) }, false, false},
		{"expired", "https://staff.example.com", "staff.example.com", 5, func(s *webauthn.SessionData) { s.Expires = time.Now().Add(-time.Second) }, false, false},
		{"wrong_owner", "https://staff.example.com", "staff.example.com", 5, func(s *webauthn.SessionData) { s.UserID = []byte("other") }, false, false},
		{"forged_signature", "https://staff.example.com", "staff.example.com", 5, nil, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, sd, e := rp.BeginLogin(owner)
			if e != nil {
				t.Fatal(e)
			}
			signer := key
			if tc.wrongKey {
				signer, e = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
				if e != nil {
					t.Fatal(e)
				}
			}
			raw := assertionFixture(t, sd, signer, id, tc.origin, tc.rpid, tc.flags, 1)
			parsed, e := protocol.ParseCredentialRequestResponseBytes(raw)
			if e != nil {
				t.Fatal(e)
			}
			if tc.mutate != nil {
				tc.mutate(sd)
			}
			_, e = rp.ValidateLogin(owner, *sd, parsed)
			if (e == nil) != tc.valid {
				t.Fatalf("valid=%v error=%v", tc.valid, e)
			}
		})
	}
}

func TestPasskeyRejectsInsecureRemoteOrigin(t *testing.T) {
	if _, err := (&Server{Origin: "http://example.com"}).passkeyRP(); err == nil {
		t.Fatal("accepted insecure remote origin")
	}
	if _, err := (&Server{Origin: "http://localhost:8080"}).passkeyRP(); err != nil {
		t.Fatal(err)
	}
}

// The following tests use the backend's isolated PostgreSQL fixture and cover
// application-level authorization and atomic challenge consumption.
func registerDatabasePasskey(t *testing.T, s *Server, srv *httptest.Server, u User, token, csrf, secret string) (string, *ecdsa.PrivateKey, []byte) {
	t.Helper()
	status, result, _ := apiRequest(t, srv, "POST", "/api/v1/security/passkeys/register/begin", token, csrf, map[string]any{"code": totpCode(secret, time.Now().Unix()/30), "label": "Security test key"})
	if status != 200 {
		t.Fatalf("begin register: %d %v", status, result)
	}
	challengeID := result["challenge_id"].(string)
	sd := databasePasskeyChallenge(t, s, challengeID)
	rp, err := s.passkeyRP()
	if err != nil {
		t.Fatal(err)
	}
	raw, key, id := registrationFixture(t, sd, s.Origin, rp.Config.RPID, 0x45)
	status, result, _ = apiRequest(t, srv, "POST", "/api/v1/security/passkeys/register/finish", token, csrf, map[string]any{"challenge_id": challengeID, "credential": json.RawMessage(raw)})
	if status != 200 {
		t.Fatalf("finish register: %d %v", status, result)
	}
	saved := result["id"].(string)
	status, _, _ = apiRequest(t, srv, "POST", "/api/v1/security/passkeys/register/finish", token, csrf, map[string]any{"challenge_id": challengeID, "credential": json.RawMessage(raw)})
	if status != 400 {
		t.Fatalf("registration replay accepted: %d", status)
	}
	return saved, key, id
}
func databasePasskeyChallenge(t *testing.T, s *Server, id string) *webauthn.SessionData {
	t.Helper()
	var raw []byte
	if err := s.Pool.QueryRow(context.Background(), "SELECT session_data FROM staff_passkey_challenges WHERE id=$1", id).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var sd webauthn.SessionData
	if err := json.Unmarshal(raw, &sd); err != nil {
		t.Fatal(err)
	}
	return &sd
}
func beginDatabasePasskeyTest(t *testing.T, s *Server, srv *httptest.Server, token, csrf string, key *ecdsa.PrivateKey, id []byte, counter uint32) (string, json.RawMessage) {
	t.Helper()
	status, result, _ := apiRequest(t, srv, "POST", "/api/v1/security/passkeys/verify/begin", token, csrf, map[string]any{})
	if status != 200 {
		t.Fatalf("begin test: %d %v", status, result)
	}
	challengeID := result["challenge_id"].(string)
	sd := databasePasskeyChallenge(t, s, challengeID)
	rp, err := s.passkeyRP()
	if err != nil {
		t.Fatal(err)
	}
	return challengeID, json.RawMessage(assertionFixture(t, sd, key, id, s.Origin, rp.Config.RPID, 5, counter))
}
func TestPasskeyDatabaseIsolationAndAtomicity(t *testing.T) {
	s, srv := testServer(t)
	secret := totpSecret()
	u := seedUser(t, s, []string{"operator"}, secret)
	token, csrf := authenticated(t, s, u)
	_, key, id := registerDatabasePasskey(t, s, srv, u, token, csrf, secret)
	challengeID, assertion := beginDatabasePasskeyTest(t, s, srv, token, csrf, key, id, 1)
	body := map[string]any{"challenge_id": challengeID, "credential": assertion}
	otherToken, otherCSRF := authenticated(t, s, u)
	status, _, _ := apiRequest(t, srv, "POST", "/api/v1/security/passkeys/verify/finish", otherToken, otherCSRF, body)
	if status != 400 {
		t.Fatalf("cross-session challenge accepted: %d", status)
	}
	other := seedUser(t, s, []string{"operator"}, totpSecret())
	otherToken, otherCSRF = authenticated(t, s, other)
	status, _, _ = apiRequest(t, srv, "POST", "/api/v1/security/passkeys/verify/finish", otherToken, otherCSRF, body)
	if status != 400 {
		t.Fatalf("cross-user challenge accepted: %d", status)
	}
	// Concurrent duplicate requests must commit exactly one completed ceremony.
	statuses := make(chan int, 6)
	start := make(chan struct{})
	for range 6 {
		go func() {
			<-start
			status, _, _ := apiRequest(t, srv, "POST", "/api/v1/security/passkeys/verify/finish", token, csrf, body)
			statuses <- status
		}()
	}
	close(start)
	success := 0
	for range 6 {
		if status := <-statuses; status == 200 {
			success++
		} else if status != 400 {
			t.Fatalf("unexpected duplicate result %d", status)
		}
	}
	if success != 1 {
		t.Fatalf("completed %d copies of same assertion", success)
	}
	challengeID, assertion = beginDatabasePasskeyTest(t, s, srv, token, csrf, key, id, 2)
	if _, err := s.Pool.Exec(context.Background(), "UPDATE staff_passkey_challenges SET expires_at=now()-interval '1 minute' WHERE id=$1", challengeID); err != nil {
		t.Fatal(err)
	}
	status, _, _ = apiRequest(t, srv, "POST", "/api/v1/security/passkeys/verify/finish", token, csrf, map[string]any{"challenge_id": challengeID, "credential": assertion})
	if status != 400 {
		t.Fatalf("expired challenge accepted: %d", status)
	}
	// Invalid cryptographic response consumes the challenge as well.
	challengeID, assertion = beginDatabasePasskeyTest(t, s, srv, token, csrf, key, id, 2)
	status, _, _ = apiRequest(t, srv, "POST", "/api/v1/security/passkeys/verify/finish", token, csrf, map[string]any{"challenge_id": challengeID, "credential": map[string]any{}})
	if status != 400 {
		t.Fatalf("invalid assertion accepted: %d", status)
	}
	status, _, _ = apiRequest(t, srv, "POST", "/api/v1/security/passkeys/verify/finish", token, csrf, map[string]any{"challenge_id": challengeID, "credential": assertion})
	if status != 400 {
		t.Fatalf("consumed invalid attempt reused: %d", status)
	}
}
func TestPasskeyDatabaseRevocationAndRecovery(t *testing.T) {
	t.Run("credential_revocation", func(t *testing.T) {
		s, srv := testServer(t)
		secret := totpSecret()
		u := seedUser(t, s, []string{"operator"}, secret)
		token, csrf := authenticated(t, s, u)
		saved, key, id := registerDatabasePasskey(t, s, srv, u, token, csrf, secret)
		challengeID, assertion := beginDatabasePasskeyTest(t, s, srv, token, csrf, key, id, 1)
		// Rewind the test-only TOTP step so this test does not sleep for a new code.
		if _, err := s.Pool.Exec(context.Background(), "UPDATE staff_users SET totp_last_step=-1 WHERE id=$1", u.ID); err != nil {
			t.Fatal(err)
		}
		status, _, _ := apiRequest(t, srv, "POST", "/api/v1/security/passkeys/"+saved+"/revoke", token, csrf, map[string]any{"code": totpCode(secret, time.Now().Unix()/30)})
		if status != 200 {
			t.Fatalf("revoke: %d", status)
		}
		status, _, _ = apiRequest(t, srv, "POST", "/api/v1/security/passkeys/verify/finish", token, csrf, map[string]any{"challenge_id": challengeID, "credential": assertion})
		if status != 400 {
			t.Fatalf("revoked challenge accepted: %d", status)
		}
		var count int
		if err := s.Pool.QueryRow(context.Background(), "SELECT count(*) FROM staff_passkeys WHERE id=$1 AND revoked_at IS NOT NULL", saved).Scan(&count); err != nil || count != 1 {
			t.Fatalf("revoked credential history lost: count=%d err=%v", count, err)
		}
	})
	t.Run("recovery_epoch", func(t *testing.T) {
		s, srv := testServer(t)
		secret := totpSecret()
		u := seedUser(t, s, []string{"operator"}, secret)
		token, csrf := authenticated(t, s, u)
		saved, key, id := registerDatabasePasskey(t, s, srv, u, token, csrf, secret)
		challengeID, assertion := beginDatabasePasskeyTest(t, s, srv, token, csrf, key, id, 1)
		if _, err := s.Pool.Exec(context.Background(), "UPDATE staff_users SET auth_epoch=auth_epoch+1 WHERE id=$1", u.ID); err != nil {
			t.Fatal(err)
		}
		status, _, _ := apiRequest(t, srv, "POST", "/api/v1/security/passkeys/verify/finish", token, csrf, map[string]any{"challenge_id": challengeID, "credential": assertion})
		if status != 401 {
			t.Fatalf("old session accepted after recovery epoch: %d", status)
		}
		u.AuthEpoch++
		token, csrf = authenticated(t, s, u)
		status, _, _ = apiRequest(t, srv, "POST", "/api/v1/security/passkeys/verify/begin", token, csrf, map[string]any{})
		if status != 400 {
			t.Fatalf("old credential accepted after recovery: %d", status)
		}
		status, result, _ := apiRequest(t, srv, "GET", "/api/v1/security/passkeys", token, csrf, nil)
		if status != 200 {
			t.Fatal(status)
		}
		items := result["items"].([]any)
		if len(items) != 1 || items[0].(map[string]any)["id"] != saved || items[0].(map[string]any)["active"] != false {
			t.Fatalf("historical credential not retained inactive: %v", result)
		}
	})
}
