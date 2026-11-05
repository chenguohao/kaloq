package server

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
	"net/http"
	"regexp"
	"strings"
)

var loginPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,63}$`)

func validRoles(roles []string, allowRoot bool) bool {
	if len(roles) == 0 || len(roles) > 6 {
		return false
	}
	seen := map[string]bool{}
	for _, r := range roles {
		if seen[r] {
			return false
		}
		seen[r] = true
		switch r {
		case "operator", "finance", "supervisor", "eo", "admin":
		case "root":
			if !allowRoot {
				return false
			}
		default:
			return false
		}
	}
	return true
}
func staffAccess(w http.ResponseWriter, u User) bool {
	if !u.HasRole("root") {
		fail(w, 403, "forbidden", "仅获授权的 Root 可维护员工")
		return false
	}
	return true
}
func (s *Server) staff(w http.ResponseWriter, r *http.Request, u User) {
	if !staffAccess(w, u) {
		return
	}
	rows, e := s.Pool.Query(r.Context(), `SELECT `+userCols+` FROM staff_users ORDER BY created_at LIMIT 200`)
	if e != nil {
		fail(w, 500, "internal", "暂时无法读取员工")
		return
	}
	defer rows.Close()
	users := []User{}
	for rows.Next() {
		v, e := scanUser(rows)
		if e != nil {
			fail(w, 500, "internal", "暂时无法读取员工")
			return
		}
		users = append(users, v)
	}
	if rows.Err() != nil {
		fail(w, 500, "internal", "暂时无法读取员工")
		return
	}
	respond(w, 200, map[string]any{"items": users})
}
func (s *Server) createStaff(w http.ResponseWriter, r *http.Request, u User) {
	if !staffAccess(w, u) {
		return
	}
	var in struct {
		LoginName   string   `json:"login_name"`
		DisplayName string   `json:"display_name"`
		Password    string   `json:"password"`
		Roles       []string `json:"roles"`
		Code        string   `json:"code"`
	}
	if !decode(w, r, &in) {
		return
	}
	in.LoginName = strings.ToLower(strings.TrimSpace(in.LoginName))
	if !loginPattern.MatchString(in.LoginName) || len(in.DisplayName) < 1 || len(in.DisplayName) > 100 || len(in.Password) < 12 || len(in.Password) > 72 || !validRoles(in.Roles, false) {
		fail(w, 400, "invalid_fields", "员工资料不正确；密码须为 12–72 字节，Root 不能通过此接口授予")
		return
	}
	if !s.allow(r.Context(), "stepup:"+u.ID, 10) {
		fail(w, 429, "rate_limited", "请稍后再试")
		return
	}
	hash, e := bcrypt.GenerateFromPassword([]byte(in.Password), 12)
	if e != nil {
		fail(w, 500, "internal", "暂时无法创建员工")
		return
	}
	var created User
	e = s.withTx(r.Context(), func(tx pgx.Tx) error {
		ctx := r.Context()
		if e := recheck(ctx, tx, u); e != nil {
			return e
		}
		if e := s.consumeTOTP(ctx, tx, u.ID, in.Code); e != nil {
			return e
		}
		id := newID()
		var e error
		created, e = scanUser(tx.QueryRow(ctx, `INSERT INTO staff_users(id,login_name,display_name,password_hash,roles) VALUES($1,$2,$3,$4,$5) RETURNING `+userCols, id, in.LoginName, in.DisplayName, string(hash), in.Roles))
		if e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `INSERT INTO staff_role_assignments(staff_id,roles,assignment_version,actor_id) VALUES($1,$2,1,$3)`, id, in.Roles, u.ID); e != nil {
			return e
		}
		return audit(ctx, tx, u.ID, "staff.created", id)
	})
	if e != nil {
		if errors.Is(e, ErrInvalidCode) {
			fail(w, 401, "invalid_code", "验证码不正确或已使用")
		} else {
			fail(w, 409, "staff_conflict", "无法创建员工，请检查登录名是否重复")
		}
		return
	}
	respond(w, 201, map[string]any{"user": created})
}
func (s *Server) updateStaff(w http.ResponseWriter, r *http.Request, u User) {
	if !staffAccess(w, u) {
		return
	}
	id := r.PathValue("id")
	if id == u.ID {
		fail(w, 403, "self_edit", "不能修改自身授权")
		return
	}
	var in struct {
		Roles  []string `json:"roles"`
		Status string   `json:"status"`
		Code   string   `json:"code"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !validRoles(in.Roles, false) || (in.Status != "active" && in.Status != "disabled") {
		fail(w, 400, "invalid_fields", "角色或状态不正确，Root 授权须通过独立流程")
		return
	}
	if !s.allow(r.Context(), "stepup:"+u.ID, 10) {
		fail(w, 429, "rate_limited", "请稍后再试")
		return
	}
	var updated User
	e := s.withTx(r.Context(), func(tx pgx.Tx) error {
		ctx := r.Context()
		if e := recheck(ctx, tx, u); e != nil {
			return e
		}
		if e := s.consumeTOTP(ctx, tx, u.ID, in.Code); e != nil {
			return e
		}
		target, e := scanUser(tx.QueryRow(ctx, `SELECT `+userCols+` FROM staff_users WHERE id::text=$1 FOR UPDATE`, id))
		if e != nil {
			return e
		}
		if target.HasRole("root") {
			return ErrForbidden
		}
		updated, e = scanUser(tx.QueryRow(ctx, `UPDATE staff_users SET roles=$2,status=$3,auth_epoch=auth_epoch+1 WHERE id=$1 RETURNING `+userCols, target.ID, in.Roles, in.Status))
		if e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `DELETE FROM staff_sessions WHERE staff_id=$1`, target.ID); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `INSERT INTO staff_role_assignments(staff_id,roles,assignment_version,actor_id) SELECT $1,$2,COALESCE(max(assignment_version),0)+1,$3 FROM staff_role_assignments WHERE staff_id=$1`, target.ID, in.Roles, u.ID); e != nil {
			return e
		}
		return audit(ctx, tx, u.ID, "staff.access_changed", target.ID)
	})
	if e != nil {
		if errors.Is(e, ErrInvalidCode) {
			fail(w, 401, "invalid_code", "验证码不正确或已使用")
		} else {
			fail(w, 403, "forbidden", "无法修改该员工授权")
		}
		return
	}
	respond(w, 200, map[string]any{"user": updated})
}

// Bootstrap creates the first Root only. It cannot overwrite or recover any existing account.
func (s *Server) Bootstrap(ctx context.Context, login, display, password string) error {
	login = strings.ToLower(strings.TrimSpace(login))
	if !loginPattern.MatchString(login) || len(display) < 1 || len(password) < 12 || len(password) > 72 {
		return errors.New("bootstrap fields invalid")
	}
	hash, e := bcrypt.GenerateFromPassword([]byte(password), 12)
	if e != nil {
		return e
	}
	return s.withTx(ctx, func(tx pgx.Tx) error {
		if _, e := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(746201106)`); e != nil {
			return e
		}
		var count int
		if e := tx.QueryRow(ctx, `SELECT count(*) FROM staff_users`).Scan(&count); e != nil {
			return e
		}
		if count != 0 {
			return errors.New("bootstrap only allowed for empty identity store")
		}
		id := newID()
		if _, e := tx.Exec(ctx, `INSERT INTO staff_users(id,login_name,display_name,password_hash,roles) VALUES($1,$2,$3,$4,ARRAY['root'])`, id, login, display, string(hash)); e != nil {
			return e
		}
		if _, e := tx.Exec(ctx, `INSERT INTO staff_role_assignments(staff_id,roles,assignment_version,actor_id) VALUES($1,ARRAY['root'],1,$1)`, id); e != nil {
			return e
		}
		return audit(ctx, tx, id, "staff.bootstrap", id)
	})
}
