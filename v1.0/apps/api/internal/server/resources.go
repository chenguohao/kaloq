package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Day 3 captures references for later review. Neither these records nor a draft
// order authorize a provider call or mark an account or destination as usable.
func (s *Server) registerResourceRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/merchants/{id}/resources", s.auth(s.resources))
	mux.HandleFunc("POST /api/v1/merchants/{id}/bindings", s.auth(s.createBinding))
	mux.HandleFunc("POST /api/v1/merchants/{id}/targets", s.auth(s.appendTarget))
	mux.HandleFunc("POST /api/v1/merchants/{id}/targets/{target}/versions", s.auth(s.appendTarget))
	mux.HandleFunc("GET /api/v1/merchants/{id}/targets/{target}/versions", s.auth(s.targetHistory))
}

var (
	resourceUUID       = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	instanceSlug       = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)
	errBindingConflict = errors.New("upstream binding conflicts with existing evidence")
	errIntakeCurrency  = errors.New("target currency conflicts with active intake setting")
	errTargetRequest   = errors.New("target request conflict")
	errResourceVersion = errors.New("resource version conflict")
)

func resourceMerchant(w http.ResponseWriter, r *http.Request, u User) (string, bool) {
	if !merchantAccess(w, u) {
		return "", false
	}
	id := r.PathValue("id")
	if !resourceUUID.MatchString(id) {
		fail(w, 404, "not_found", "商户不存在")
		return "", false
	}
	return id, true
}

func resourceError(w http.ResponseWriter, err error) {
	var pgErr *pgconn.PgError
	switch {
	case errors.Is(err, ErrForbidden):
		fail(w, 403, "forbidden", "员工授权已变化，请重新登录")
	case errors.Is(err, pgx.ErrNoRows):
		fail(w, 404, "not_found", "商户或其关联资料不存在")
	case errors.Is(err, errBindingConflict):
		fail(w, 409, "binding_conflict", "通道实例或来源资源与已有登记不一致，请复核来源记录")
	case errors.Is(err, errIntakeCurrency):
		fail(w, 400, "invalid_fields", "请按当前环境业务币种登记银行目标")
	case errors.Is(err, errTargetRequest):
		fail(w, 409, "request_conflict", "该请求编号已用于其他资料或操作")
	case errors.Is(err, errResourceVersion):
		fail(w, 409, "version_conflict", "资料版本已变化，请重新读取后重试")
	case errors.As(err, &pgErr) && pgErr.Code == "23503":
		fail(w, 400, "resource_mismatch", "请选择本商户及同一通道实例下的有效资料")
	default:
		fail(w, 500, "internal", "暂时无法读取或登记资料")
	}
}

func validResourceText(value string, limit int) bool {
	return value != "" && value == strings.TrimSpace(value) && len(value) <= limit && strings.IndexFunc(value, unicode.IsControl) < 0
}

func maskedResource(value string) string {
	runes := []rune(value)
	if len(runes) <= 4 {
		return "••••"
	}
	return "••••" + string(runes[len(runes)-4:])
}

// Use a keyed lookup instead of a plaintext account number or guessable raw hash.
func (s *Server) resourceKey(domain, value string) []byte {
	h := hmac.New(sha256.New, s.Key)
	h.Write([]byte("resource-binding/" + domain + "\x00" + value))
	return h.Sum(nil)
}

func resourceRows[T any](ctx context.Context, tx pgx.Tx, query string, args ...any) ([]T, error) {
	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[T])
}

type resourceProfile struct {
	Version   int64     `json:"version"`
	LegalName string    `json:"legal_name"`
	CreatedAt time.Time `json:"created_at"`
}

type resourceBinding struct {
	ID               string    `json:"id"`
	InstanceID       string    `json:"instance_id"`
	ProviderCode     string    `json:"provider_code"`
	InstanceCode     string    `json:"instance_code"`
	Environment      string    `json:"environment"`
	Kind             string    `json:"kind"`
	Label            string    `json:"label"`
	MaskedExternalID string    `json:"masked_external_id"`
	Asset            string    `json:"asset"`
	ProfileVersion   int64     `json:"profile_version"`
	ParentID         *string   `json:"parent_binding_id,omitempty"`
	ReviewState      string    `json:"review_state"`
	CreatedAt        time.Time `json:"created_at"`
}

const bindingColumns = `b.id,b.instance_id,i.provider_code,i.instance_code,i.environment,b.kind,b.label,b.masked_external_id,b.asset,b.profile_version,b.parent_binding_id,b.review_state,b.created_at`

type resourceTarget struct {
	ID                string    `json:"id"`
	TargetID          string    `json:"target_id"`
	Version           int64     `json:"version"`
	Kind              string    `json:"kind"`
	Label             string    `json:"label"`
	MaskedDestination string    `json:"masked_destination"`
	Asset             string    `json:"asset"`
	Network           string    `json:"network"`
	ReviewState       string    `json:"review_state"`
	CreatedAt         time.Time `json:"created_at"`
}

const targetColumns = `id,target_id,version,kind,label,masked_destination,asset,network,review_state,created_at`

func (s *Server) resources(w http.ResponseWriter, r *http.Request, u User) {
	merchant, ok := resourceMerchant(w, r, u)
	if !ok {
		return
	}
	ctx := r.Context()
	tx, err := s.Pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		resourceError(w, err)
		return
	}
	defer tx.Rollback(ctx)
	profiles, err := resourceRows[resourceProfile](ctx, tx, `SELECT version,legal_name,created_at FROM merchant_profile_versions WHERE merchant_id=$1 ORDER BY version DESC`, merchant)
	if err != nil {
		resourceError(w, err)
		return
	}
	if len(profiles) == 0 {
		resourceError(w, pgx.ErrNoRows)
		return
	}
	bindings, err := resourceRows[resourceBinding](ctx, tx, `SELECT `+bindingColumns+` FROM provider_bindings b JOIN provider_instances i ON i.id=b.instance_id WHERE b.merchant_id=$1 ORDER BY b.created_at DESC,b.id`, merchant)
	if err != nil {
		resourceError(w, err)
		return
	}
	targets, err := resourceRows[resourceTarget](ctx, tx, `SELECT DISTINCT ON (target_id) `+targetColumns+` FROM payment_target_versions WHERE merchant_id=$1 ORDER BY target_id,version DESC`, merchant)
	if err != nil {
		resourceError(w, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		resourceError(w, err)
		return
	}
	respond(w, 200, map[string]any{"fiat_asset": s.intakeFiatAsset(), "profiles": profiles, "bindings": bindings, "targets": targets})
}

func (s *Server) targetHistory(w http.ResponseWriter, r *http.Request, u User) {
	merchant, ok := resourceMerchant(w, r, u)
	if !ok {
		return
	}
	id := r.PathValue("target")
	if !resourceUUID.MatchString(id) {
		resourceError(w, pgx.ErrNoRows)
		return
	}
	rows, err := s.Pool.Query(r.Context(), `SELECT `+targetColumns+` FROM payment_target_versions WHERE merchant_id=$1 AND target_id=$2 ORDER BY version DESC`, merchant, id)
	if err != nil {
		resourceError(w, err)
		return
	}
	items, err := pgx.CollectRows(rows, pgx.RowToStructByPos[resourceTarget])
	if err != nil {
		resourceError(w, err)
		return
	}
	if len(items) == 0 {
		resourceError(w, pgx.ErrNoRows)
		return
	}
	respond(w, 200, map[string]any{"items": items})
}

type bindingInput struct {
	ProviderCode   string `json:"provider_code"`
	InstanceCode   string `json:"instance_code"`
	Kind           string `json:"kind"`
	Label          string `json:"label"`
	AccountScope   string `json:"account_scope"`
	ExternalID     string `json:"external_id"`
	EvidenceRef    string `json:"evidence_ref"`
	ProfileVersion int64  `json:"profile_version"`
	Asset          string `json:"asset"`
	ParentID       string `json:"parent_binding_id"`
}

func (in bindingInput) valid() bool {
	if !instanceSlug.MatchString(in.ProviderCode) || !instanceSlug.MatchString(in.InstanceCode) || in.ProfileVersion < 1 || !validResourceText(in.Label, 120) || !validResourceText(in.ExternalID, 256) || !validResourceText(in.EvidenceRef, 512) {
		return false
	}
	if in.Kind == "enrollment" {
		return in.Asset == "" && in.ParentID == "" && validResourceText(in.AccountScope, 160)
	}
	return in.Kind == "virtual_account" && (in.Asset == "HKD" || in.Asset == "USD") && resourceUUID.MatchString(in.ParentID) && (in.AccountScope == "" || validResourceText(in.AccountScope, 160))
}

// The operator supplies only an observation. Creating the instance identity here
// provisions no adapter credentials or executable capability.
func (s *Server) createBinding(w http.ResponseWriter, r *http.Request, u User) {
	merchant, ok := resourceMerchant(w, r, u)
	if !ok {
		return
	}
	var in bindingInput
	if !decode(w, r, &in) {
		return
	}
	if !in.valid() {
		fail(w, 400, "invalid_fields", "请核对实例、开户归属、资料版本、资产及凭证引用")
		return
	}
	ctx := r.Context()
	var binding resourceBinding
	created := false
	err := s.withTx(ctx, func(tx pgx.Tx) error {
		if err := recheck(ctx, tx, u); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO provider_instances(id,provider_code,instance_code,environment,created_by) VALUES($1,$2,$3,'sandbox',$4) ON CONFLICT(environment,instance_code) DO NOTHING`, newID(), in.ProviderCode, in.InstanceCode, u.ID)
		if err != nil {
			return err
		}
		var instance, provider string
		err = tx.QueryRow(ctx, `SELECT id,provider_code FROM provider_instances WHERE environment='sandbox' AND instance_code=$1`, in.InstanceCode).Scan(&instance, &provider)
		if err != nil {
			return err
		}
		if provider != in.ProviderCode {
			return errBindingConflict
		}
		var parent *string
		scope := in.AccountScope
		if in.Kind == "virtual_account" {
			parent = &in.ParentID
			scope = in.ParentID
		}
		scopeKey := s.resourceKey("scope", scope)
		externalKey := s.resourceKey("external", in.ExternalID)
		// Include every user-supplied attribute in encrypted provenance, so a strict
		// replay returns the first record without permitting silent edits or rebinds.
		payload, err := json.Marshal(in)
		if err != nil {
			return err
		}
		enc, err := s.encrypt(payload)
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `INSERT INTO provider_bindings(id,merchant_id,instance_id,kind,label,account_scope,external_key,masked_external_id,details_enc,profile_version,parent_binding_id,asset,created_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) ON CONFLICT(instance_id,kind,account_scope,external_key) DO NOTHING`, newID(), merchant, instance, in.Kind, in.Label, scopeKey, externalKey, maskedResource(in.ExternalID), enc, in.ProfileVersion, parent, in.Asset, u.ID)
		if err != nil {
			return err
		}
		created = tag.RowsAffected() == 1
		var id string
		var saved []byte
		err = tx.QueryRow(ctx, `SELECT id,details_enc FROM provider_bindings WHERE merchant_id=$1 AND instance_id=$2 AND kind=$3 AND account_scope=$4 AND external_key=$5`, merchant, instance, in.Kind, scopeKey, externalKey).Scan(&id, &saved)
		if errors.Is(err, pgx.ErrNoRows) {
			return errBindingConflict
		}
		if err != nil {
			return err
		}
		if !created {
			raw, err := s.decrypt(saved)
			if err != nil {
				return err
			}
			var prior bindingInput
			if err = json.Unmarshal(raw, &prior); err != nil {
				return err
			}
			if prior != in {
				return errBindingConflict
			}
		}
		rows, err := tx.Query(ctx, `SELECT `+bindingColumns+` FROM provider_bindings b JOIN provider_instances i ON i.id=b.instance_id WHERE b.id=$1 AND b.merchant_id=$2`, id, merchant)
		if err != nil {
			return err
		}
		binding, err = pgx.CollectExactlyOneRow(rows, pgx.RowToStructByPos[resourceBinding])
		if err != nil {
			return err
		}
		if created {
			return audit(ctx, tx, u.ID, "binding.recorded", id)
		}
		return nil
	})
	if err != nil {
		resourceError(w, err)
		return
	}
	status := 200
	if created {
		status = 201
	}
	respond(w, status, map[string]any{"binding": binding})
}

type targetInput struct {
	RequestID       string `json:"request_id"`
	ExpectedVersion int64  `json:"expected_version"`
	Kind            string `json:"kind"`
	Label           string `json:"label"`
	Destination     string `json:"destination"`
	Asset           string `json:"asset"`
	Network         string `json:"network"`
	EvidenceRef     string `json:"evidence_ref"`
}

func (in targetInput) valid() bool {
	if !resourceUUID.MatchString(in.RequestID) {
		return false
	}
	if !validResourceText(in.Label, 120) || !validResourceText(in.Destination, 512) || !validResourceText(in.EvidenceRef, 512) {
		return false
	}
	return (in.Kind == "bank" && (in.Asset == "USD" || in.Asset == "HKD") && in.Network == "") || (in.Kind == "chain" && in.Asset == "USDT" && validResourceText(in.Network, 64))
}
func (s *Server) appendTarget(w http.ResponseWriter, r *http.Request, u User) {
	merchant, ok := resourceMerchant(w, r, u)
	if !ok {
		return
	}
	var in targetInput
	if !decode(w, r, &in) {
		return
	}
	id := r.PathValue("target")
	if id != "" && !resourceUUID.MatchString(id) {
		resourceError(w, pgx.ErrNoRows)
		return
	}
	if !in.valid() || (id == "" && in.ExpectedVersion != 0) || (id != "" && in.ExpectedVersion < 1) {
		fail(w, 400, "invalid_fields", "请核对目标类型、版本、币种、网络和凭证引用")
		return
	}
	ctx := r.Context()
	payload, err := json.Marshal(in)
	if err != nil {
		resourceError(w, err)
		return
	}
	digest := s.resourceKey("target-request", u.ID+"/"+merchant+"/"+id+"/"+string(payload))
	var target resourceTarget
	created := false
	err = s.withTx(ctx, func(tx pgx.Tx) error {
		if err := recheck(ctx, tx, u); err != nil {
			return err
		}
		// Serialize exact retries independently of competing edits to the target.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,3097))`, merchant+in.RequestID); err != nil {
			return err
		}
		var priorID string
		var priorDigest []byte
		err := tx.QueryRow(ctx, `SELECT id,request_digest FROM payment_target_versions WHERE merchant_id=$1 AND request_id=$2`, merchant, in.RequestID).Scan(&priorID, &priorDigest)
		if err == nil {
			if !hmac.Equal(digest, priorDigest) {
				return errTargetRequest
			}
			rows, e := tx.Query(ctx, `SELECT `+targetColumns+` FROM payment_target_versions WHERE id=$1`, priorID)
			if e != nil {
				return e
			}
			target, e = pgx.CollectExactlyOneRow(rows, pgx.RowToStructByPos[resourceTarget])
			return e
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if in.Kind == "bank" && in.Asset != s.intakeFiatAsset() {
			return errIntakeCurrency
		}
		if id == "" {
			id = newID()
			_, err := tx.Exec(ctx, `INSERT INTO payment_targets(id,merchant_id,kind,created_by) VALUES($1,$2,$3,$4)`, id, merchant, in.Kind, u.ID)
			if err != nil {
				return err
			}
		} else {
			// Runtime has insert-only permissions on immutable target records.
			if err := lockTargetVersions(ctx, tx, id); err != nil {
				return err
			}
			var current int64
			var kind string
			err := tx.QueryRow(ctx, `SELECT version,kind FROM payment_target_versions WHERE merchant_id=$1 AND target_id=$2 ORDER BY version DESC LIMIT 1`, merchant, id).Scan(&current, &kind)
			if err != nil {
				return err
			}
			if current != in.ExpectedVersion || kind != in.Kind {
				return errResourceVersion
			}
		}
		raw, err := json.Marshal(map[string]string{"destination": in.Destination, "evidence_ref": in.EvidenceRef})
		if err != nil {
			return err
		}
		enc, err := s.encrypt(raw)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `INSERT INTO payment_target_versions(id,merchant_id,target_id,version,kind,label,asset,network,masked_destination,details_enc,created_by,request_id,request_digest) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING `+targetColumns, newID(), merchant, id, in.ExpectedVersion+1, in.Kind, in.Label, in.Asset, in.Network, maskedResource(in.Destination), enc, u.ID, in.RequestID, digest)
		if err != nil {
			return err
		}
		target, err = pgx.CollectExactlyOneRow(rows, pgx.RowToStructByPos[resourceTarget])
		if err != nil {
			return err
		}
		created = true
		return audit(ctx, tx, u.ID, "target.version_recorded", target.ID)
	})
	if err != nil {
		resourceError(w, err)
		return
	}
	status := 200
	if created {
		status = 201
	}
	respond(w, status, map[string]any{"target": target})
}

// Shared with draft intake so a concurrent target edit cannot supersede a
// freshly checked destination between validation and order commit.
func lockTargetVersions(ctx context.Context, tx pgx.Tx, id string) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 3086))`, id)
	return err
}
