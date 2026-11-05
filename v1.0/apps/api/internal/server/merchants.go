package server

import (
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"net/http"
	"net/mail"
	"strings"
	"time"
)

type Merchant struct {
	ID           string    `json:"id"`
	Code         string    `json:"merchant_code"`
	LegalName    string    `json:"legal_name"`
	ContactName  string    `json:"contact_name"`
	ContactEmail string    `json:"contact_email"`
	Status       string    `json:"service_status"`
	Version      int64     `json:"row_version"`
	CreatedAt    time.Time `json:"created_at"`
}
type merchantInput struct {
	LegalName    string `json:"legal_name"`
	ContactName  string `json:"contact_name"`
	ContactEmail string `json:"contact_email"`
	Status       string `json:"service_status"`
	Version      int64  `json:"row_version"`
}

func validMerchant(in *merchantInput) bool {
	in.LegalName = strings.TrimSpace(in.LegalName)
	in.ContactName = strings.TrimSpace(in.ContactName)
	in.ContactEmail = strings.TrimSpace(in.ContactEmail)
	addr, e := mail.ParseAddress(in.ContactEmail)
	return e == nil && addr.Address == in.ContactEmail && len(in.ContactEmail) <= 254 && len(in.LegalName) > 0 && len(in.LegalName) <= 200 && len(in.ContactName) > 0 && len(in.ContactName) <= 100
}
func merchantAccess(w http.ResponseWriter, u User) bool {
	if !u.HasRole("operator") {
		fail(w, 403, "forbidden", "当前角色无商户操作权限")
		return false
	}
	return true
}
func (s *Server) readMerchant(row pgx.Row) (m Merchant, err error) {
	var enc []byte
	err = row.Scan(&m.ID, &m.Code, &m.LegalName, &enc, &m.Status, &m.Version, &m.CreatedAt)
	if err != nil {
		return
	}
	var b []byte
	b, err = s.decrypt(enc)
	if err != nil {
		return
	}
	var contact struct {
		Name  string
		Email string
	}
	err = json.Unmarshal(b, &contact)
	m.ContactName = contact.Name
	m.ContactEmail = contact.Email
	return
}

const merchantCols = `id,merchant_code,legal_name,contact_enc,service_status,row_version,created_at`

func (s *Server) merchants(w http.ResponseWriter, r *http.Request, u User) {
	if !merchantAccess(w, u) {
		return
	}
	q := r.URL.Query().Get("q")
	status := r.URL.Query().Get("status")
	if len(q) > 200 {
		fail(w, 400, "invalid_query", "查询过长")
		return
	}
	rows, e := s.Pool.Query(r.Context(), `SELECT `+merchantCols+` FROM merchants WHERE ($1='' OR strpos(lower(legal_name),lower($1))>0 OR strpos(merchant_code,upper($1))>0) AND ($2='' OR service_status=$2) ORDER BY created_at DESC LIMIT 200`, q, status)
	if e != nil {
		fail(w, 500, "internal", "暂时无法读取商户")
		return
	}
	defer rows.Close()
	items := []Merchant{}
	for rows.Next() {
		m, e := s.readMerchant(rows)
		if e != nil {
			fail(w, 500, "internal", "暂时无法读取商户")
			return
		}
		items = append(items, m)
	}
	if rows.Err() != nil {
		fail(w, 500, "internal", "暂时无法读取商户")
		return
	}
	respond(w, 200, map[string]any{"items": items})
}
func (s *Server) merchant(w http.ResponseWriter, r *http.Request, u User) {
	if !merchantAccess(w, u) {
		return
	}
	m, e := s.readMerchant(s.Pool.QueryRow(r.Context(), `SELECT `+merchantCols+` FROM merchants WHERE id::text=$1`, r.PathValue("id")))
	if e != nil {
		fail(w, 404, "not_found", "商户不存在")
		return
	}
	respond(w, 200, map[string]any{"merchant": m})
}
func (s *Server) createMerchant(w http.ResponseWriter, r *http.Request, u User) {
	if !merchantAccess(w, u) {
		return
	}
	var in merchantInput
	if !decode(w, r, &in) {
		return
	}
	if !validMerchant(&in) || (in.Status != "" && in.Status != "draft") {
		fail(w, 400, "invalid_fields", "请填写商户名称、联系人和有效邮箱，初始状态须为草稿")
		return
	}
	s.saveMerchant(w, r, u, in, true)
}
func (s *Server) updateMerchant(w http.ResponseWriter, r *http.Request, u User) {
	if !merchantAccess(w, u) {
		return
	}
	var in merchantInput
	if !decode(w, r, &in) {
		return
	}
	if !validMerchant(&in) || in.Version < 1 || (in.Status != "draft" && in.Status != "active" && in.Status != "paused") {
		fail(w, 400, "invalid_fields", "商户资料、状态或版本不正确")
		return
	}
	s.saveMerchant(w, r, u, in, false)
}
func (s *Server) saveMerchant(w http.ResponseWriter, r *http.Request, u User, in merchantInput, create bool) {
	ctx := r.Context()
	var m Merchant
	raw, _ := json.Marshal(struct {
		Name  string
		Email string
	}{in.ContactName, in.ContactEmail})
	enc, e := s.encrypt(raw)
	if e != nil {
		fail(w, 500, "internal", "暂时无法保存商户")
		return
	}
	e = s.withTx(ctx, func(tx pgx.Tx) error {
		if e := recheck(ctx, tx, u); e != nil {
			return e
		}
		var row pgx.Row
		if create {
			id := newID()
			code := "M-" + strings.ToUpper(strings.ReplaceAll(id, "-", ""))
			row = tx.QueryRow(ctx, `INSERT INTO merchants(id,merchant_code,legal_name,contact_enc,created_by) VALUES($1,$2,$3,$4,$5) RETURNING `+merchantCols, id, code, in.LegalName, enc, u.ID)
		} else {
			row = tx.QueryRow(ctx, `UPDATE merchants SET legal_name=$2,contact_enc=$3,service_status=$4,row_version=row_version+1,updated_at=now() WHERE id::text=$1 AND row_version=$5 RETURNING `+merchantCols, r.PathValue("id"), in.LegalName, enc, in.Status, in.Version)
		}
		m, e = s.readMerchant(row)
		if e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `INSERT INTO merchant_profile_versions(merchant_id,version,legal_name,contact_enc,actor_id) VALUES($1,$2,$3,$4,$5)`, m.ID, m.Version, m.LegalName, enc, u.ID); e != nil {
			return e
		}
		action := "merchant.updated"
		if create {
			action = "merchant.created"
		}
		return audit(ctx, tx, u.ID, action, m.ID)
	})
	if e != nil {
		if e == pgx.ErrNoRows {
			fail(w, 409, "version_conflict", "资料已更新，请刷新后重试")
		} else {
			fail(w, 500, "internal", "暂时无法保存商户")
		}
		return
	}
	code := 200
	if create {
		code = 201
	}
	respond(w, code, map[string]any{"merchant": m})
}
