package server

import (
	"bytes"
	"context"
	"encoding/json"
	"golang.org/x/crypto/bcrypt"
	"io"
	"kaloq/v1/internal/store"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func testServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL required for isolated PostgreSQL integration checks")
	}
	pool, e := store.Open(context.Background(), dsn)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(pool.Close)
	if !strings.HasSuffix(pool.Config().ConnConfig.Database, "_test") {
		t.Fatal("integration tests require an isolated database ending in _test")
	}
	if e = store.Migrate(context.Background(), pool, "../../../../database/migrations"); e != nil {
		t.Fatal(e)
	}
	if _, e = pool.Exec(context.Background(), `TRUNCATE staff_users,auth_rate_limits RESTART IDENTITY CASCADE`); e != nil {
		t.Fatal(e)
	}
	s := &Server{Pool: pool, Key: bytes.Repeat([]byte{42}, 32), Origin: "http://localhost"}
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	return s, srv
}
func seedUser(t *testing.T, s *Server, roles []string, secret string) User {
	t.Helper()
	u := User{ID: newID(), LoginName: "test-" + randomToken()[:10], DisplayName: "Test employee", Roles: roles, Status: "active", AuthEpoch: 1}
	hash, e := bcrypt.GenerateFromPassword([]byte("testing-password-123"), bcrypt.MinCost)
	if e != nil {
		t.Fatal(e)
	}
	var enc []byte
	if secret != "" {
		enc, e = s.encrypt([]byte(secret))
		if e != nil {
			t.Fatal(e)
		}
	}
	_, e = s.Pool.Exec(context.Background(), `INSERT INTO staff_users(id,login_name,display_name,password_hash,roles,totp_secret_enc) VALUES($1,$2,$3,$4,$5,$6)`, u.ID, u.LoginName, u.DisplayName, string(hash), roles, enc)
	if e != nil {
		t.Fatal(e)
	}
	return u
}
func authenticated(t *testing.T, s *Server, u User) (string, string) {
	t.Helper()
	token, csrf := randomToken(), randomToken()
	_, e := s.Pool.Exec(context.Background(), `INSERT INTO staff_sessions(token_hash,staff_id,auth_epoch,stage,csrf_token,expires_at) VALUES($1,$2,$3,'full',$4,now()+interval '1 hour')`, hashToken(token), u.ID, u.AuthEpoch, csrf)
	if e != nil {
		t.Fatal(e)
	}
	return token, csrf
}
func apiRequest(t *testing.T, srv *httptest.Server, method, path, token, csrf string, body any) (int, map[string]any, *http.Response) {
	t.Helper()
	b, _ := json.Marshal(body)
	req, e := http.NewRequest(method, srv.URL+path, bytes.NewReader(b))
	if e != nil {
		t.Fatal(e)
	}
	req.Header.Set("Origin", "http://localhost")
	req.Header.Set("Content-Type", "application/json")
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	if token != "" {
		req.AddCookie(&http.Cookie{Name: "kaloq_v1_session", Value: token})
	}
	res, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	out := map[string]any{}
	raw, e := io.ReadAll(res.Body)
	if e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(raw, &out); e != nil {
		t.Fatalf("invalid JSON response status %d: %v", res.StatusCode, e)
	}
	return res.StatusCode, out, res
}
func cookieFrom(t *testing.T, res *http.Response) string {
	t.Helper()
	for _, c := range res.Cookies() {
		if c.Name == "kaloq_v1_session" {
			if !c.HttpOnly || c.SameSite != http.SameSiteStrictMode {
				t.Fatal("unsafe cookie")
			}
			return c.Value
		}
	}
	t.Fatal("missing session cookie")
	return ""
}
func TestIdentityEnrollmentRecoveryAndMerchantIsolation(t *testing.T) {
	s, srv := testServer(t)
	u := seedUser(t, s, []string{"operator"}, "")
	status, out, res := apiRequest(t, srv, "POST", "/api/v1/auth/login", "", "", map[string]any{"login_name": u.LoginName, "password": "testing-password-123"})
	if status != 200 || out["stage"] != "enroll" {
		t.Fatalf("login %d", status)
	}
	challenge := cookieFrom(t, res)
	status, _, _ = apiRequest(t, srv, "GET", "/api/v1/merchants", challenge, "", nil)
	if status != 401 {
		t.Fatal("challenge bypassed 2FA")
	}
	status, out, _ = apiRequest(t, srv, "GET", "/api/v1/auth/enrollment", challenge, "", nil)
	if status != 200 {
		t.Fatal(status)
	}
	secret := out["secret"].(string)
	status, _, _ = apiRequest(t, srv, "POST", "/api/v1/auth/enrollment", challenge, "", map[string]string{"code": "wrong"})
	if status != 401 {
		t.Fatal("wrong OTP accepted")
	}
	status, out, res = apiRequest(t, srv, "POST", "/api/v1/auth/enrollment", challenge, "", map[string]string{"code": totpCode(secret, time.Now().Unix()/30)})
	if status != 200 {
		t.Fatal(status)
	}
	full := cookieFrom(t, res)
	recovery := out["recovery_codes"].([]any)[0].(string)
	status, out, _ = apiRequest(t, srv, "GET", "/api/v1/me", full, "", nil)
	if status != 200 {
		t.Fatal(status)
	}
	csrf := out["csrf_token"].(string)
	input := map[string]any{"legal_name": "Example business", "contact_name": "Test contact", "contact_email": "test@example.com"}
	status, _, _ = apiRequest(t, srv, "POST", "/api/v1/merchants", full, "", input)
	if status != 403 {
		t.Fatal("missing CSRF accepted")
	}
	status, out, _ = apiRequest(t, srv, "POST", "/api/v1/merchants", full, csrf, input)
	if status != 201 {
		t.Fatal(status)
	}
	merchant := out["merchant"].(map[string]any)
	id := merchant["id"].(string)
	var enc []byte
	if e := s.Pool.QueryRow(context.Background(), `SELECT contact_enc FROM merchants WHERE id=$1`, id).Scan(&enc); e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(enc, []byte("test@example.com")) {
		t.Fatal("contact unencrypted")
	}
	input["service_status"] = "active"
	input["row_version"] = 1
	status, _, _ = apiRequest(t, srv, "PATCH", "/api/v1/merchants/"+id, full, csrf, input)
	if status != 200 {
		t.Fatal(status)
	}
	status, _, _ = apiRequest(t, srv, "PATCH", "/api/v1/merchants/"+id, full, csrf, input)
	if status != 409 {
		t.Fatal("stale edit accepted")
	}
	root := seedUser(t, s, []string{"root"}, totpSecret())
	rt, rc := authenticated(t, s, root)
	status, _, _ = apiRequest(t, srv, "GET", "/api/v1/merchants", rt, rc, nil)
	if status != 403 {
		t.Fatal("root inherited business access")
	}
	status, _, res = apiRequest(t, srv, "POST", "/api/v1/auth/login", "", "", map[string]any{"login_name": u.LoginName, "password": "testing-password-123"})
	if status != 200 {
		t.Fatal(status)
	}
	challenge = cookieFrom(t, res)
	status, _, res = apiRequest(t, srv, "POST", "/api/v1/auth/recover", challenge, "", map[string]string{"recovery_code": recovery})
	if status != 200 {
		t.Fatal(status)
	}
	rebind := cookieFrom(t, res)
	status, _, _ = apiRequest(t, srv, "GET", "/api/v1/me", full, "", nil)
	if status != 401 {
		t.Fatal("old full session survived recovery")
	}
	status, _, _ = apiRequest(t, srv, "GET", "/api/v1/me", rebind, "", nil)
	if status != 401 {
		t.Fatal("recovery issued business session")
	}
	status, _, _ = apiRequest(t, srv, "POST", "/api/v1/auth/recover", challenge, "", map[string]string{"recovery_code": recovery})
	if status != 401 {
		t.Fatal("recovery reused")
	}
	var n int
	s.Pool.QueryRow(context.Background(), `SELECT count(*) FROM recovery_codes WHERE staff_id=$1`, u.ID).Scan(&n)
	if n != 0 {
		t.Fatal("old recovery codes retained")
	}
	var epochs int64
	s.Pool.QueryRow(context.Background(), `SELECT auth_epoch FROM staff_users WHERE id=$1`, u.ID).Scan(&epochs)
	if epochs < 3 {
		t.Fatal("epoch not revoked")
	}
}
func TestConcurrentOTPOnlyOnceAndChallengeExpiry(t *testing.T) {
	s, srv := testServer(t)
	secret := totpSecret()
	u := seedUser(t, s, []string{"operator"}, secret)
	tokens := []string{}
	for range 2 {
		status, _, res := apiRequest(t, srv, "POST", "/api/v1/auth/login", "", "", map[string]any{"login_name": u.LoginName, "password": "testing-password-123"})
		if status != 200 {
			t.Fatal(status)
		}
		tokens = append(tokens, cookieFrom(t, res))
	}
	codes := make(chan int, 2)
	var wg sync.WaitGroup
	for _, token := range tokens {
		wg.Add(1)
		go func(token string) {
			defer wg.Done()
			status, _, _ := apiRequest(t, srv, "POST", "/api/v1/auth/totp", token, "", map[string]string{"code": totpCode(secret, time.Now().Unix()/30)})
			codes <- status
		}(token)
	}
	wg.Wait()
	close(codes)
	success := 0
	for status := range codes {
		if status == 200 {
			success++
		} else if status != 401 {
			t.Fatalf("unexpected %d", status)
		}
	}
	if success != 1 {
		t.Fatalf("OTP success count %d", success)
	}
	_, _, res := apiRequest(t, srv, "POST", "/api/v1/auth/login", "", "", map[string]any{"login_name": u.LoginName, "password": "testing-password-123"})
	token := cookieFrom(t, res)
	s.Pool.Exec(context.Background(), `UPDATE staff_sessions SET expires_at=now()-interval '1 second' WHERE token_hash=$1`, hashToken(token))
	status, _, _ := apiRequest(t, srv, "POST", "/api/v1/auth/totp", token, "", map[string]string{"code": totpCode(secret, time.Now().Unix()/30+1)})
	if status != 401 {
		t.Fatal("expired challenge accepted")
	}
}
func TestStaffRevocationAndMaintenanceBoundaries(t *testing.T) {
	s, srv := testServer(t)
	secret := totpSecret()
	root := seedUser(t, s, []string{"root"}, secret)
	rt, rc := authenticated(t, s, root)
	operator := seedUser(t, s, []string{"operator"}, totpSecret())
	ot, oc := authenticated(t, s, operator)
	update := map[string]any{"roles": []string{"operator"}, "status": "disabled", "code": totpCode(secret, time.Now().Unix()/30)}
	status, _, _ := apiRequest(t, srv, "PATCH", "/api/v1/staff/"+operator.ID, rt, rc, update)
	if status != 200 {
		t.Fatal(status)
	}
	status, _, _ = apiRequest(t, srv, "GET", "/api/v1/merchants", ot, oc, nil)
	if status != 401 {
		t.Fatal("disabled staff session survived")
	}
	status, _, _ = apiRequest(t, srv, "PATCH", "/api/v1/staff/"+root.ID, rt, rc, update)
	if status != 403 {
		t.Fatal("root edited itself")
	}
	status, _, _ = apiRequest(t, srv, "POST", "/api/v1/staff", rt, rc, map[string]any{"login_name": "new-root", "display_name": "Root", "password": "some-password-123", "roles": []string{"root"}, "code": "000000"})
	if status != 400 {
		t.Fatal("root grant allowed")
	}
	if e := s.Bootstrap(context.Background(), "another-root", "Root", "some-password-123"); e == nil {
		t.Fatal("bootstrap allowed after accounts exist")
	}
}
func TestOriginAndChallengeLimits(t *testing.T) {
	s, srv := testServer(t)
	u := seedUser(t, s, []string{"operator"}, totpSecret())
	_, _, res := apiRequest(t, srv, "POST", "/api/v1/auth/login", "", "", map[string]any{"login_name": u.LoginName, "password": "testing-password-123"})
	token := cookieFrom(t, res)
	for i := 0; i < 6; i++ {
		status, _, _ := apiRequest(t, srv, "POST", "/api/v1/auth/totp", token, "", map[string]string{"code": "wrong"})
		if status != 401 {
			t.Fatal(status)
		}
	}
	var attempts int
	s.Pool.QueryRow(context.Background(), `SELECT attempts FROM staff_sessions WHERE token_hash=$1`, hashToken(token)).Scan(&attempts)
	if attempts != 5 {
		t.Fatalf("challenge attempts %d", attempts)
	}
	req, _ := http.NewRequest("POST", srv.URL+"/api/v1/auth/login", strings.NewReader(`{}`))
	req.Header.Set("Origin", "https://attacker.invalid")
	req.Header.Set("Content-Type", "application/json")
	reply, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	reply.Body.Close()
	if reply.StatusCode != 403 {
		t.Fatal("cross-site request accepted")
	}
}
func TestMigrationChecksumsAndDBGuard(t *testing.T) {
	s, _ := testServer(t)
	if p, e := store.Open(context.Background(), "postgres://localhost/kaloq_demo"); e == nil {
		p.Close()
		t.Fatal("legacy DB accepted")
	}
	dir := t.TempDir()
	raw, e := os.ReadFile("../../../../database/migrations/001_identity_merchants.sql")
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(dir, "001_identity_merchants.sql"), append(raw, []byte("\n-- changed")...), 0600); e != nil {
		t.Fatal(e)
	}
	if e = store.Migrate(context.Background(), s.Pool, dir); e == nil {
		t.Fatal("migration drift accepted")
	}
}
