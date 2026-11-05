package server

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
	"net"
	"net/http"
	"net/url"
	"strings"
)

var ErrInvalidCode = errors.New("invalid code")

func (s *Server) consumeTOTP(ctx context.Context, tx pgx.Tx, staffID, code string) error {
	var enc []byte
	var last int64
	if e := tx.QueryRow(ctx, `SELECT totp_secret_enc,totp_last_step FROM staff_users WHERE id=$1 AND status='active' FOR UPDATE`, staffID).Scan(&enc, &last); e != nil {
		return ErrInvalidCode
	}
	secret, e := s.decrypt(enc)
	if e != nil {
		return ErrInvalidCode
	}
	step, ok := verifyTOTP(string(secret), code, last)
	if !ok {
		return ErrInvalidCode
	}
	_, e = tx.Exec(ctx, `UPDATE staff_users SET totp_last_step=$2 WHERE id=$1`, staffID, step)
	return e
}
func (s *Server) allow(ctx context.Context, key string, limit int) bool {
	var n int
	e := s.Pool.QueryRow(ctx, `INSERT INTO auth_rate_limits(key,window_at,attempts) VALUES($1,now(),1) ON CONFLICT(key) DO UPDATE SET window_at=CASE WHEN auth_rate_limits.window_at<now()-interval '5 minutes' THEN now() ELSE auth_rate_limits.window_at END,attempts=CASE WHEN auth_rate_limits.window_at<now()-interval '5 minutes' THEN 1 ELSE auth_rate_limits.attempts+1 END RETURNING attempts`, key).Scan(&n)
	return e == nil && n <= limit
}
func (s *Server) requestIP(r *http.Request) string {
	host, _, e := net.SplitHostPort(r.RemoteAddr)
	if e != nil {
		return r.RemoteAddr
	}
	if s.TrustProxyLoopback && net.ParseIP(host).IsLoopback() {
		if ip := net.ParseIP(r.Header.Get("X-Real-IP")); ip != nil {
			return ip.String()
		}
	}
	return host
}
func (s *Server) limited(w http.ResponseWriter, r *http.Request, identity string) bool {
	if !s.allow(r.Context(), "ip:"+hashToken(s.requestIP(r)), 100) || !s.allow(r.Context(), "account:"+hashToken(identity), 10) {
		w.Header().Set("Retry-After", "300")
		fail(w, 429, "rate_limited", "尝试次数过多，请稍后再试")
		return true
	}
	return false
}
func scanUser(row pgx.Row) (u User, err error) {
	err = row.Scan(&u.ID, &u.LoginName, &u.DisplayName, &u.Roles, &u.Status, &u.AuthEpoch)
	return
}

const userCols = `id,login_name,display_name,roles,status,auth_epoch`

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		LoginName string `json:"login_name"`
		Password  string `json:"password"`
	}
	if !decode(w, r, &in) {
		return
	}
	in.LoginName = strings.ToLower(strings.TrimSpace(in.LoginName))
	if s.limited(w, r, in.LoginName) {
		return
	}
	var u User
	var passwordHash string
	var enc []byte
	err := s.Pool.QueryRow(r.Context(), `SELECT `+userCols+`,password_hash,totp_secret_enc FROM staff_users WHERE login_name=$1`, in.LoginName).Scan(&u.ID, &u.LoginName, &u.DisplayName, &u.Roles, &u.Status, &u.AuthEpoch, &passwordHash, &enc)
	if err != nil {
		bcrypt.CompareHashAndPassword([]byte("$2a$12$M4vf6eun0Ibw2MZ5eqDQmeKc/UHwX2EPWxeS3YpJdFJ6heA8RxAPe"), []byte(in.Password))
		fail(w, 401, "invalid_credentials", "登录名或密码不正确")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(in.Password)) != nil || u.Status != "active" {
		fail(w, 401, "invalid_credentials", "登录名或密码不正确")
		return
	}
	stage := "totp"
	var enrollment []byte
	if len(enc) == 0 {
		stage = "enroll"
		enrollment, err = s.encrypt([]byte(totpSecret()))
		if err != nil {
			fail(w, 500, "internal", "暂时无法登录")
			return
		}
	}
	token := randomToken()
	err = s.withTx(r.Context(), func(tx pgx.Tx) error {
		if e := recheck(r.Context(), tx, u); e != nil {
			return e
		}
		_, e := tx.Exec(r.Context(), `INSERT INTO staff_sessions(token_hash,staff_id,auth_epoch,stage,csrf_token,enrollment_secret_enc,expires_at) VALUES($1,$2,$3,$4,$5,$6,now()+interval '5 minutes')`, hashToken(token), u.ID, u.AuthEpoch, stage, randomToken(), enrollment)
		return e
	})
	if err != nil {
		fail(w, 500, "internal", "暂时无法登录")
		return
	}
	s.cookie(w, token, 300)
	respond(w, 200, map[string]string{"stage": stage})
}

type challenge struct {
	User     User
	Stage    string
	Enc      []byte
	Attempts int
}

func (s *Server) challenge(ctx context.Context, tx pgx.Tx, r *http.Request) (c challenge, e error) {
	e = tx.QueryRow(ctx, `SELECT u.id,u.login_name,u.display_name,u.roles,u.status,u.auth_epoch,s.stage,s.enrollment_secret_enc,s.attempts FROM staff_sessions s JOIN staff_users u ON u.id=s.staff_id WHERE s.token_hash=$1 AND s.expires_at>now() AND s.auth_epoch=u.auth_epoch AND u.status='active' AND s.stage IN ('enroll','totp') AND s.attempts<5 FOR UPDATE OF u,s`, hashToken(cookieToken(r))).Scan(&c.User.ID, &c.User.LoginName, &c.User.DisplayName, &c.User.Roles, &c.User.Status, &c.User.AuthEpoch, &c.Stage, &c.Enc, &c.Attempts)
	return
}
func (s *Server) enrollment(w http.ResponseWriter, r *http.Request) {
	var secret []byte
	var login string
	e := s.withTx(r.Context(), func(tx pgx.Tx) error {
		c, e := s.challenge(r.Context(), tx, r)
		if e != nil || c.Stage != "enroll" {
			return ErrForbidden
		}
		login = c.User.LoginName
		secret, e = s.decrypt(c.Enc)
		return e
	})
	if e != nil {
		fail(w, 401, "challenge_required", "请重新登录后绑定认证器")
		return
	}
	uri := "otpauth://totp/" + url.PathEscape("Kaloq:"+login) + "?secret=" + url.QueryEscape(string(secret)) + "&issuer=Kaloq&algorithm=SHA1&digits=6&period=30"
	respond(w, 200, map[string]string{"secret": string(secret), "otpauth_uri": uri})
}
func (s *Server) failedChallenge(ctx context.Context, tx pgx.Tx, r *http.Request) error {
	_, e := tx.Exec(ctx, `UPDATE staff_sessions SET attempts=attempts+1 WHERE token_hash=$1`, hashToken(cookieToken(r)))
	return e
}
func (s *Server) fullSession(ctx context.Context, tx pgx.Tx, u User, old string) (string, error) {
	token := randomToken()
	_, e := tx.Exec(ctx, `DELETE FROM staff_sessions WHERE token_hash=$1`, hashToken(old))
	if e != nil {
		return "", e
	}
	_, e = tx.Exec(ctx, `INSERT INTO staff_sessions(token_hash,staff_id,auth_epoch,stage,csrf_token,expires_at) VALUES($1,$2,$3,'full',$4,now()+interval '8 hours')`, hashToken(token), u.ID, u.AuthEpoch, randomToken())
	return token, e
}
func (s *Server) enroll(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Code string `json:"code"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !s.allow(r.Context(), "verify-ip:"+hashToken(s.requestIP(r)), 100) {
		fail(w, 429, "rate_limited", "请稍后再试")
		return
	}
	var token string
	var u User
	codes := []string{}
	invalid := false
	err := s.withTx(r.Context(), func(tx pgx.Tx) error {
		ctx := r.Context()
		c, e := s.challenge(ctx, tx, r)
		if e != nil || c.Stage != "enroll" {
			return ErrForbidden
		}
		u = c.User
		secret, e := s.decrypt(c.Enc)
		if e != nil {
			return e
		}
		step, ok := verifyTOTP(string(secret), in.Code, -1)
		if !ok {
			invalid = true
			return s.failedChallenge(ctx, tx, r)
		}
		// A successful enrollment invalidates all parallel challenges; the winning secret is immutable thereafter.
		enc, e := s.encrypt(secret)
		if e != nil {
			return e
		}
		u.AuthEpoch++
		if _, e = tx.Exec(ctx, `UPDATE staff_users SET totp_secret_enc=$2,totp_last_step=$3,auth_epoch=$4 WHERE id=$1`, u.ID, enc, step, u.AuthEpoch); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `DELETE FROM staff_sessions WHERE staff_id=$1`, u.ID); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `DELETE FROM recovery_codes WHERE staff_id=$1`, u.ID); e != nil {
			return e
		}
		for range 8 {
			code := randomToken()[:24]
			codes = append(codes, code)
			if _, e = tx.Exec(ctx, `INSERT INTO recovery_codes(staff_id,code_hash) VALUES($1,$2)`, u.ID, hashToken(code)); e != nil {
				return e
			}
		}
		token, e = s.fullSession(ctx, tx, u, "")
		if e != nil {
			return e
		}
		return audit(ctx, tx, u.ID, "totp.enrolled", u.ID)
	})
	if err != nil {
		fail(w, 401, "challenge_required", "认证已失效，请重新登录")
		return
	}
	if invalid {
		fail(w, 401, "invalid_code", "验证码不正确或已使用")
		return
	}
	s.cookie(w, token, 28800)
	respond(w, 200, map[string]any{"user": u, "recovery_codes": codes})
}
func (s *Server) verify(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Code string `json:"code"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !s.allow(r.Context(), "verify-ip:"+hashToken(s.requestIP(r)), 100) {
		fail(w, 429, "rate_limited", "请稍后再试")
		return
	}
	var identity string
	if e := s.Pool.QueryRow(r.Context(), `SELECT staff_id::text FROM staff_sessions WHERE token_hash=$1`, hashToken(cookieToken(r))).Scan(&identity); e != nil {
		fail(w, 401, "challenge_required", "请重新登录")
		return
	}
	if !s.allow(r.Context(), "totp:"+identity, 10) {
		fail(w, 429, "rate_limited", "请稍后再试")
		return
	}
	var u User
	var token string
	invalid := false
	err := s.withTx(r.Context(), func(tx pgx.Tx) error {
		ctx := r.Context()
		c, e := s.challenge(ctx, tx, r)
		if e != nil || c.Stage != "totp" {
			return ErrForbidden
		}
		u = c.User
		if e = s.consumeTOTP(ctx, tx, u.ID, in.Code); e != nil {
			invalid = true
			return s.failedChallenge(ctx, tx, r)
		}
		token, e = s.fullSession(ctx, tx, u, cookieToken(r))
		if e != nil {
			return e
		}
		return audit(ctx, tx, u.ID, "auth.login", u.ID)
	})
	if err != nil {
		fail(w, 401, "challenge_required", "认证已失效，请重新登录")
		return
	}
	if invalid {
		fail(w, 401, "invalid_code", "验证码不正确或已使用")
		return
	}
	s.cookie(w, token, 28800)
	respond(w, 200, map[string]any{"user": u})
}
func (s *Server) recover(w http.ResponseWriter, r *http.Request) {
	var in struct {
		RecoveryCode string `json:"recovery_code"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !s.allow(r.Context(), "recover-ip:"+hashToken(s.requestIP(r)), 20) {
		fail(w, 429, "rate_limited", "请稍后再试")
		return
	}
	invalid := false
	token := randomToken()
	err := s.withTx(r.Context(), func(tx pgx.Tx) error {
		ctx := r.Context()
		c, e := s.challenge(ctx, tx, r)
		if e != nil || c.Stage != "totp" {
			return ErrForbidden
		}
		tag, e := tx.Exec(ctx, `DELETE FROM recovery_codes WHERE staff_id=$1 AND code_hash=$2`, c.User.ID, hashToken(strings.TrimSpace(in.RecoveryCode)))
		if e != nil {
			return e
		}
		if tag.RowsAffected() != 1 {
			invalid = true
			return s.failedChallenge(ctx, tx, r)
		}
		if _, e = tx.Exec(ctx, `DELETE FROM recovery_codes WHERE staff_id=$1`, c.User.ID); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `DELETE FROM staff_sessions WHERE staff_id=$1`, c.User.ID); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `UPDATE staff_users SET auth_epoch=auth_epoch+1,totp_secret_enc=NULL,totp_last_step=-1 WHERE id=$1`, c.User.ID); e != nil {
			return e
		}
		enc, e := s.encrypt([]byte(totpSecret()))
		if e != nil {
			return e
		}
		_, e = tx.Exec(ctx, `INSERT INTO staff_sessions(token_hash,staff_id,auth_epoch,stage,csrf_token,enrollment_secret_enc,expires_at) VALUES($1,$2,$3,'enroll',$4,$5,now()+interval '5 minutes')`, hashToken(token), c.User.ID, c.User.AuthEpoch+1, randomToken(), enc)
		if e != nil {
			return e
		}
		return audit(ctx, tx, c.User.ID, "auth.recovered", c.User.ID)
	})
	if err != nil {
		fail(w, 401, "challenge_required", "请重新登录后恢复")
		return
	}
	if invalid {
		fail(w, 401, "invalid_code", "恢复码不正确或已使用")
		return
	}
	s.cookie(w, token, 300)
	respond(w, 200, map[string]string{"stage": "enroll"})
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	_, e := s.Pool.Exec(r.Context(), `DELETE FROM staff_sessions WHERE token_hash=$1`, hashToken(cookieToken(r)))
	if e != nil {
		fail(w, 500, "internal", "暂时无法退出")
		return
	}
	s.cookie(w, "", -1)
	respond(w, 200, map[string]any{})
}
