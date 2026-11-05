package server

// Passkeys are enrolled by an authenticated employee for a credential signing
// test. These endpoints do not authorize an order, transfer, or login session.
import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/jackc/pgx/v5"
)

type passkeyUser struct {
	User
	Credentials []webauthn.Credential
}

func (u passkeyUser) WebAuthnID() []byte                         { return []byte(u.ID) }
func (u passkeyUser) WebAuthnName() string                       { return u.LoginName }
func (u passkeyUser) WebAuthnDisplayName() string                { return u.DisplayName }
func (u passkeyUser) WebAuthnCredentials() []webauthn.Credential { return u.Credentials }

func (s *Server) passkeyRP() (*webauthn.WebAuthn, error) {
	origin, err := url.Parse(s.Origin)
	if err != nil || origin.Hostname() == "" || (origin.Scheme != "https" && !(origin.Scheme == "http" && origin.Hostname() == "localhost")) {
		return nil, errors.New("passkeys require HTTPS or localhost origin")
	}
	return webauthn.New(&webauthn.Config{
		RPID: origin.Hostname(), RPDisplayName: "Kaloq", RPOrigins: []string{s.Origin},
		AuthenticatorSelection: protocol.AuthenticatorSelection{ResidentKey: protocol.ResidentKeyRequirementPreferred, UserVerification: protocol.VerificationRequired},
		AttestationPreference:  protocol.PreferNoAttestation,
		Timeouts: webauthn.TimeoutsConfig{
			Registration: webauthn.TimeoutConfig{Enforce: true, Timeout: 5 * time.Minute},
			Login:        webauthn.TimeoutConfig{Enforce: true, Timeout: 5 * time.Minute},
		},
	})
}

func (s *Server) registerPasskeyRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/v1/security/passkeys", s.auth(s.listPasskeys))
	m.HandleFunc("POST /api/v1/security/passkeys/register/begin", s.auth(s.beginPasskeyRegistration))
	m.HandleFunc("POST /api/v1/security/passkeys/register/finish", s.auth(s.finishPasskeyRegistration))
	m.HandleFunc("POST /api/v1/security/passkeys/verify/begin", s.auth(s.beginPasskeyTest))
	m.HandleFunc("POST /api/v1/security/passkeys/verify/finish", s.auth(s.finishPasskeyTest))
	m.HandleFunc("POST /api/v1/security/passkeys/{id}/revoke", s.auth(s.revokePasskey))
}

func passkeyRandomID() (string, error) {
	b := make([]byte, 32)
	_, err := rand.Read(b)
	return hex.EncodeToString(b), err
}

// Serialize changes against the staff row. Recheck the live session within the
// same transaction to prevent an in-flight ceremony racing account revocation.
func (s *Server) lockPasskeyUser(ctx context.Context, tx pgx.Tx, r *http.Request, u User) (string, error) {
	cookie, err := r.Cookie("kaloq_v1_session")
	if err != nil {
		return "", err
	}
	token := hashToken(cookie.Value)
	var epoch int64
	var status string
	if err = tx.QueryRow(ctx, "SELECT auth_epoch,status FROM staff_users WHERE id=$1 FOR UPDATE", u.ID).Scan(&epoch, &status); err != nil {
		return "", err
	}
	if epoch != u.AuthEpoch || status != "active" {
		return "", errors.New("identity revoked")
	}
	var exists bool
	err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM staff_sessions WHERE token_hash=$1 AND staff_id=$2 AND auth_epoch=$3 AND stage='full' AND expires_at>now())", token, u.ID, u.AuthEpoch).Scan(&exists)
	if err != nil || !exists {
		return "", errors.New("session revoked")
	}
	return token, nil
}

func loadPasskeys(ctx context.Context, tx pgx.Tx, u User, rp string) (passkeyUser, error) {
	out := passkeyUser{User: u}
	rows, err := tx.Query(ctx, "SELECT credential FROM staff_passkeys WHERE staff_id=$1 AND auth_epoch=$2 AND rp_id=$3 AND revoked_at IS NULL ORDER BY id", u.ID, u.AuthEpoch, rp)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		var c webauthn.Credential
		if err = rows.Scan(&raw); err != nil {
			return out, err
		}
		if err = json.Unmarshal(raw, &c); err != nil {
			return out, err
		}
		out.Credentials = append(out.Credentials, c)
	}
	return out, rows.Err()
}

func (s *Server) listPasskeys(w http.ResponseWriter, r *http.Request, u User) {
	rp, err := s.passkeyRP()
	if err != nil {
		fail(w, 503, "passkeys_unavailable", "签名凭证配置暂不可用")
		return
	}
	rows, err := s.Pool.Query(r.Context(), "SELECT id,label,created_at,revoked_at,(revoked_at IS NULL AND auth_epoch=$2 AND rp_id=$3) FROM staff_passkeys WHERE staff_id=$1 ORDER BY created_at DESC", u.ID, u.AuthEpoch, rp.Config.RPID)
	if err != nil {
		fail(w, 500, "storage_error", "暂时无法读取凭证")
		return
	}
	defer rows.Close()
	type item struct {
		ID        string     `json:"id"`
		Label     string     `json:"label"`
		CreatedAt time.Time  `json:"created_at"`
		RevokedAt *time.Time `json:"revoked_at,omitempty"`
		Active    bool       `json:"active"`
	}
	items := []item{}
	for rows.Next() {
		var v item
		if err = rows.Scan(&v.ID, &v.Label, &v.CreatedAt, &v.RevokedAt, &v.Active); err != nil {
			fail(w, 500, "storage_error", "暂时无法读取凭证")
			return
		}
		items = append(items, v)
	}
	if rows.Err() != nil {
		fail(w, 500, "storage_error", "暂时无法读取凭证")
		return
	}
	respond(w, 200, map[string]any{"items": items, "rp_id": rp.Config.RPID, "purpose": "credential_test"})
}

func (s *Server) beginPasskeyRegistration(w http.ResponseWriter, r *http.Request, u User) {
	var input struct {
		Code  string `json:"code"`
		Label string `json:"label"`
	}
	if !decode(w, r, &input) {
		return
	}
	if s.limited(w, r, u.LoginName) {
		return
	}
	input.Label = strings.TrimSpace(input.Label)
	if input.Label == "" || len([]rune(input.Label)) > 80 {
		fail(w, 400, "invalid_label", "请输入 1–80 字的凭证名称")
		return
	}
	s.beginPasskey(w, r, u, "register", input.Code, input.Label)
}
func (s *Server) beginPasskeyTest(w http.ResponseWriter, r *http.Request, u User) {
	s.beginPasskey(w, r, u, "credential_test", "", "")
}

func (s *Server) beginPasskey(w http.ResponseWriter, r *http.Request, u User, purpose, code, label string) {
	rp, err := s.passkeyRP()
	if err != nil {
		fail(w, 503, "passkeys_unavailable", "签名凭证配置暂不可用")
		return
	}
	id, err := passkeyRandomID()
	if err != nil {
		fail(w, 500, "random_error", "暂时无法开始凭证验证")
		return
	}
	var publicKey any
	var rejection error
	err = s.withTx(r.Context(), func(tx pgx.Tx) error {
		sessionHash, e := s.lockPasskeyUser(r.Context(), tx, r, u)
		if e != nil {
			return e
		}
		owner, e := loadPasskeys(r.Context(), tx, u, rp.Config.RPID)
		if e != nil {
			return e
		}
		if purpose == "register" && len(owner.Credentials) >= 10 {
			rejection = errors.New("credential limit")
			return nil
		}
		var sd *webauthn.SessionData
		if purpose == "register" {
			if e = s.consumeTOTP(r.Context(), tx, u.ID, code); e != nil {
				rejection = e
				return nil
			}
			exclusions := make([]protocol.CredentialDescriptor, 0, len(owner.Credentials))
			for _, c := range owner.Credentials {
				exclusions = append(exclusions, c.Descriptor())
			}
			var options *protocol.CredentialCreation
			options, sd, e = rp.BeginRegistration(owner, webauthn.WithExclusions(exclusions), webauthn.WithRegistrationOrigin(s.Origin))
			if e != nil {
				return e
			}
			publicKey = options.Response
		} else {
			if len(owner.Credentials) == 0 {
				rejection = errors.New("no credentials")
				return nil
			}
			var options *protocol.CredentialAssertion
			options, sd, e = rp.BeginLogin(owner, webauthn.WithUserVerification(protocol.VerificationRequired), webauthn.WithLoginOrigin(s.Origin))
			if e != nil {
				return e
			}
			publicKey = options.Response
		}
		raw, e := json.Marshal(sd)
		if e != nil {
			return e
		}
		// Bound pending storage to the active session/purpose; a new begin supersedes
		// any previous unfinished ceremony for the same purpose.
		_, e = tx.Exec(r.Context(), "DELETE FROM staff_passkey_challenges WHERE session_hash=$1 AND purpose=$2", sessionHash, purpose)
		if e != nil {
			return e
		}
		_, e = tx.Exec(r.Context(), "INSERT INTO staff_passkey_challenges(id,staff_id,auth_epoch,session_hash,purpose,label,session_data,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,now()+interval '5 minutes')", id, u.ID, u.AuthEpoch, sessionHash, purpose, label, raw)
		return e
	})
	if rejection != nil {
		fail(w, 400, "passkey_begin_rejected", "动态码无效、暂无可用凭证或已达到凭证上限")
		return
	}
	if err != nil {
		fail(w, 400, "passkey_begin_rejected", "无法开始凭证验证，请重新登录后重试")
		return
	}
	respond(w, 200, map[string]any{"challenge_id": id, "publicKey": publicKey})
}

func (s *Server) finishPasskeyRegistration(w http.ResponseWriter, r *http.Request, u User) {
	s.finishPasskey(w, r, u, "register")
}
func (s *Server) finishPasskeyTest(w http.ResponseWriter, r *http.Request, u User) {
	s.finishPasskey(w, r, u, "credential_test")
}
func (s *Server) finishPasskey(w http.ResponseWriter, r *http.Request, u User, purpose string) {
	var input struct {
		ChallengeID string          `json:"challenge_id"`
		Credential  json.RawMessage `json:"credential"`
	}
	if !decode(w, r, &input) {
		return
	}
	rp, err := s.passkeyRP()
	if err != nil {
		fail(w, 503, "passkeys_unavailable", "签名凭证配置暂不可用")
		return
	}
	var result any
	var invalid error
	err = s.withTx(r.Context(), func(tx pgx.Tx) error {
		sessionHash, e := s.lockPasskeyUser(r.Context(), tx, r, u)
		if e != nil {
			return e
		}
		var raw []byte
		var label string
		e = tx.QueryRow(r.Context(), "UPDATE staff_passkey_challenges SET consumed_at=now() WHERE id=$1 AND staff_id=$2 AND auth_epoch=$3 AND session_hash=$4 AND purpose=$5 AND consumed_at IS NULL AND expires_at>now() RETURNING session_data,label", input.ChallengeID, u.ID, u.AuthEpoch, sessionHash, purpose).Scan(&raw, &label)
		if e != nil {
			return e
		}
		var sd webauthn.SessionData
		if e = json.Unmarshal(raw, &sd); e != nil {
			return e
		}
		owner, e := loadPasskeys(r.Context(), tx, u, rp.Config.RPID)
		if e != nil {
			return e
		}
		var credential *webauthn.Credential
		if purpose == "register" {
			var parsed *protocol.ParsedCredentialCreationData
			parsed, e = protocol.ParseCredentialCreationResponseBytes(input.Credential)
			if e == nil {
				credential, e = rp.CreateCredential(owner, sd, parsed)
			}
		} else {
			var parsed *protocol.ParsedCredentialAssertionData
			parsed, e = protocol.ParseCredentialRequestResponseBytes(input.Credential)
			if e == nil {
				credential, e = rp.ValidateLogin(owner, sd, parsed)
			}
		}
		if e != nil {
			invalid = e
			return audit(r.Context(), tx, u.ID, "passkey."+purpose+".rejected", u.ID)
		}
		raw, e = json.Marshal(credential)
		if e != nil {
			return e
		}
		if purpose == "register" {
			if len(owner.Credentials) >= 10 {
				invalid = errors.New("credential limit")
				return nil
			}
			id, e := passkeyRandomID()
			if e != nil {
				return e
			}
			tag, e := tx.Exec(r.Context(), "INSERT INTO staff_passkeys(id,staff_id,auth_epoch,rp_id,label,credential_id,credential) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT (rp_id,credential_id) DO NOTHING", id, u.ID, u.AuthEpoch, rp.Config.RPID, label, credential.ID, raw)
			if e != nil {
				return e
			}
			if tag.RowsAffected() != 1 {
				invalid = errors.New("duplicate credential")
				return nil
			}
			result = map[string]any{"id": id, "label": label}
		} else {
			// Retain the updated signature counter/backup flags returned by the library.
			if credential.Authenticator.CloneWarning {
				invalid = errors.New("counter anomaly")
				return audit(r.Context(), tx, u.ID, "passkey.counter_anomaly", u.ID)
			}
			tag, e := tx.Exec(r.Context(), "UPDATE staff_passkeys SET credential=$1 WHERE staff_id=$2 AND auth_epoch=$3 AND rp_id=$4 AND credential_id=$5 AND revoked_at IS NULL", raw, u.ID, u.AuthEpoch, rp.Config.RPID, credential.ID)
			if e != nil {
				return e
			}
			if tag.RowsAffected() != 1 {
				return errors.New("credential revoked")
			}
			result = map[string]any{"verified": true, "purpose": "credential_test"}
		}
		return audit(r.Context(), tx, u.ID, "passkey."+purpose+".completed", u.ID)
	})
	if invalid != nil || err != nil {
		fail(w, 400, "passkey_verification_failed", "凭证验证失败或挑战已失效，请重新开始")
		return
	}
	respond(w, 200, result)
}

func (s *Server) revokePasskey(w http.ResponseWriter, r *http.Request, u User) {
	var input struct {
		Code string `json:"code"`
	}
	if !decode(w, r, &input) {
		return
	}
	if s.limited(w, r, u.LoginName) {
		return
	}
	var invalid error
	err := s.withTx(r.Context(), func(tx pgx.Tx) error {
		if _, e := s.lockPasskeyUser(r.Context(), tx, r, u); e != nil {
			return e
		}
		if e := s.consumeTOTP(r.Context(), tx, u.ID, input.Code); e != nil {
			invalid = e
			return nil
		}
		tag, e := tx.Exec(r.Context(), "UPDATE staff_passkeys SET revoked_at=now() WHERE id=$1 AND staff_id=$2 AND revoked_at IS NULL", r.PathValue("id"), u.ID)
		if e != nil {
			return e
		}
		if tag.RowsAffected() != 1 {
			invalid = errors.New("not found")
			return nil
		}
		_, e = tx.Exec(r.Context(), "DELETE FROM staff_passkey_challenges WHERE staff_id=$1", u.ID)
		if e != nil {
			return e
		}
		return audit(r.Context(), tx, u.ID, "passkey.revoked", r.PathValue("id"))
	})
	if invalid != nil || err != nil {
		fail(w, 400, "passkey_revoke_rejected", "凭证不存在或动态码无效")
		return
	}
	respond(w, 200, map[string]any{"ok": true})
}
