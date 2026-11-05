package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"net/http"
	"strings"
	"time"
)

type Server struct {
	Pool               *pgxpool.Pool
	Key                []byte
	Origin             string
	CookieSecure       bool
	CookiePath         string
	WebDir             string
	TrustProxyLoopback bool
}
type User struct {
	ID          string   `json:"id"`
	LoginName   string   `json:"login_name"`
	DisplayName string   `json:"display_name"`
	Roles       []string `json:"roles"`
	Status      string   `json:"status"`
	AuthEpoch   int64    `json:"-"`
}

func (u User) HasRole(role string) bool {
	for _, r := range u.Roles {
		if r == role {
			return true
		}
	}
	return false
}
func respond(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, status int, code, message string) {
	respond(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}
func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 32768)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(dst) != nil {
		fail(w, 400, "invalid_request", "请求内容不正确")
		return false
	}
	if d.Decode(&struct{}{}) != io.EOF {
		fail(w, 400, "invalid_request", "请求内容不正确")
		return false
	}
	return true
}
func randomToken() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	h := hex.EncodeToString(b[:])
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}
func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}
func audit(ctx context.Context, tx pgx.Tx, actor, action, target string) error {
	var who any
	if actor != "" {
		who = actor
	}
	_, e := tx.Exec(ctx, `INSERT INTO audit_events(actor_id,action,target_id) VALUES($1,$2,$3)`, who, action, target)
	return e
}
func (s *Server) withTx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if e = fn(tx); e != nil {
		return e
	}
	return tx.Commit(ctx)
}

var ErrForbidden = errors.New("forbidden")

func recheck(ctx context.Context, tx pgx.Tx, u User) error {
	var epoch int64
	var status string
	if e := tx.QueryRow(ctx, `SELECT auth_epoch,status FROM staff_users WHERE id=$1 FOR UPDATE`, u.ID).Scan(&epoch, &status); e != nil {
		return e
	}
	if epoch != u.AuthEpoch || status != "active" {
		return ErrForbidden
	}
	return nil
}
func cookieToken(r *http.Request) string {
	c, e := r.Cookie("kaloq_v1_session")
	if e != nil {
		return ""
	}
	return c.Value
}
func (s *Server) cookie(w http.ResponseWriter, token string, seconds int) {
	path := s.CookiePath
	if path == "" {
		path = "/"
	}
	http.SetCookie(w, &http.Cookie{Name: "kaloq_v1_session", Value: token, Path: path, MaxAge: seconds, HttpOnly: true, Secure: s.CookieSecure, SameSite: http.SameSiteStrictMode})
}
func (s *Server) auth(fn func(http.ResponseWriter, *http.Request, User)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var u User
		var csrf string
		err := s.Pool.QueryRow(r.Context(), `SELECT u.id,u.login_name,u.display_name,u.roles,u.status,u.auth_epoch,s.csrf_token FROM staff_sessions s JOIN staff_users u ON u.id=s.staff_id WHERE s.token_hash=$1 AND s.stage='full' AND s.expires_at>now() AND s.auth_epoch=u.auth_epoch AND u.status='active'`, hashToken(cookieToken(r))).Scan(&u.ID, &u.LoginName, &u.DisplayName, &u.Roles, &u.Status, &u.AuthEpoch, &csrf)
		if err != nil {
			fail(w, 401, "unauthenticated", "请先完成登录与双因素认证")
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" && r.Header.Get("X-CSRF-Token") != csrf {
			fail(w, 403, "csrf", "请求验证失败，请刷新页面")
			return
		}
		fn(w, r, u)
	}
}
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		if s.Pool.Ping(ctx) != nil {
			fail(w, 503, "unavailable", "服务暂不可用")
			return
		}
		respond(w, 200, map[string]string{"status": "ok", "service": "kaloq-v1", "stage": "day2"})
	})
	mux.HandleFunc("POST /api/v1/auth/login", s.login)
	mux.HandleFunc("GET /api/v1/auth/enrollment", s.enrollment)
	mux.HandleFunc("POST /api/v1/auth/enrollment", s.enroll)
	mux.HandleFunc("POST /api/v1/auth/totp", s.verify)
	mux.HandleFunc("POST /api/v1/auth/recover", s.recover)
	mux.HandleFunc("POST /api/v1/auth/logout", s.logout)
	mux.HandleFunc("GET /api/v1/me", s.auth(func(w http.ResponseWriter, r *http.Request, u User) {
		var csrf string
		s.Pool.QueryRow(r.Context(), `SELECT csrf_token FROM staff_sessions WHERE token_hash=$1`, hashToken(cookieToken(r))).Scan(&csrf)
		respond(w, 200, map[string]any{"user": u, "csrf_token": csrf})
	}))
	mux.HandleFunc("GET /api/v1/security", s.auth(func(w http.ResponseWriter, r *http.Request, u User) {
		respond(w, 200, map[string]any{"totp_enabled": true, "manual_recovery_enabled": false})
	}))
	mux.HandleFunc("GET /api/v1/merchants", s.auth(s.merchants))
	mux.HandleFunc("POST /api/v1/merchants", s.auth(s.createMerchant))
	mux.HandleFunc("GET /api/v1/merchants/{id}", s.auth(s.merchant))
	mux.HandleFunc("PATCH /api/v1/merchants/{id}", s.auth(s.updateMerchant))
	mux.HandleFunc("GET /api/v1/staff", s.auth(s.staff))
	mux.HandleFunc("POST /api/v1/staff", s.auth(s.createStaff))
	mux.HandleFunc("PATCH /api/v1/staff/{id}", s.auth(s.updateStaff))
	s.registerPasskeyRoutes(mux)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { fail(w, 404, "not_found", "接口不存在") })
	if s.WebDir != "" {
		mux.Handle("/", http.FileServer(http.Dir(s.WebDir)))
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; object-src 'none'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		if r.Method != "GET" && r.Method != "HEAD" {
			if r.Header.Get("Origin") != s.Origin {
				fail(w, 403, "origin", "请求来源不正确")
				return
			}
			if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
				fail(w, 415, "content_type", "需要 JSON 请求")
				return
			}
		}
		mux.ServeHTTP(w, r)
	})
}
