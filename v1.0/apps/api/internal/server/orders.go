package server

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// These are intake units. Network-specific asset and location authorization happens before dispatch.
type orderDirection struct {
	source, destination           string
	sourceScale, destinationScale int
}

func (s *Server) intakeFiatAsset() string {
	if s.IntakeFiatAsset == "" {
		return "USD"
	}
	return s.IntakeFiatAsset
}
func (in orderInput) assets() orderDirection {
	if in.Direction == "fiat_to_crypto" {
		return orderDirection{in.FiatAsset, "USDT", 2, 6}
	}
	return orderDirection{"USDT", in.FiatAsset, 6, 2}
}

var orderUUID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
var plainDecimal = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?$`)

type quoteInput struct {
	Rate              string    `json:"rate"`
	DestinationAmount string    `json:"destination_amount"`
	ExpiresAt         time.Time `json:"expires_at"`
	Evidence          string    `json:"evidence"`
}
type orderInput struct {
	RequestID        string      `json:"request_id"`
	ExpectedVersion  int64       `json:"expected_version,omitempty"`
	Direction        string      `json:"direction"`
	FiatAsset        string      `json:"fiat_asset"`
	SourceAmount     string      `json:"source_amount"`
	Instruction      string      `json:"instruction"`
	ProfileVersion   int64       `json:"profile_version"`
	TargetVersionID  *string     `json:"target_version_id,omitempty"`
	ChannelBindingID *string     `json:"channel_binding_id,omitempty"`
	Quote            *quoteInput `json:"quote,omitempty"`
}
type orderProblem struct {
	status        int
	code, message string
}

func (e *orderProblem) Error() string { return e.code }
func badOrder(message string) error   { return &orderProblem{400, "invalid_fields", message} }
func writeOrderError(w http.ResponseWriter, err error) {
	var p *orderProblem
	switch {
	case errors.As(err, &p):
		fail(w, p.status, p.code, p.message)
	case errors.Is(err, pgx.ErrNoRows):
		fail(w, 404, "not_found", "商户或业务单不存在")
	case errors.Is(err, ErrForbidden):
		fail(w, 403, "forbidden", "员工授权已变更，请重新登录")
	default:
		fail(w, 500, "internal", "暂时无法处理业务单")
	}
}

// Convert a nonzero positive decimal string to an exact integer, without rounding.
func parseOrderAmount(value string, scale int) (string, bool) {
	if len(value) > 80 || !plainDecimal.MatchString(value) {
		return "", false
	}
	whole, fraction, _ := strings.Cut(value, ".")
	if len(fraction) > scale {
		return "", false
	}
	minor := strings.TrimLeft(whole+fraction+strings.Repeat("0", scale-len(fraction)), "0")
	return minor, len(minor) > 0 && len(minor) <= 38
}
func displayOrderAmount(minor string, scale int) string {
	if scale == 0 {
		return minor
	}
	if len(minor) <= scale {
		minor = strings.Repeat("0", scale+1-len(minor)) + minor
	}
	split := len(minor) - scale
	return strings.TrimRight(strings.TrimRight(minor[:split]+"."+minor[split:], "0"), ".")
}
func (in *orderInput) validate(revision bool) error {
	spec := in.assets()
	if in.FiatAsset != "USD" && in.FiatAsset != "HKD" {
		return badOrder("业务法币必须是 HKD 或 USD")
	}
	if in.Direction != "fiat_to_crypto" && in.Direction != "crypto_to_fiat" {
		return badOrder("请选择业务方向")
	}
	if !orderUUID.MatchString(in.RequestID) {
		return badOrder("缺少有效的请求编号")
	}
	in.RequestID = strings.ToLower(in.RequestID)
	if in.ProfileVersion < 1 || (revision && in.ExpectedVersion < 1) || (!revision && in.ExpectedVersion != 0) {
		return badOrder("资料或业务单版本不正确")
	}
	for _, id := range []*string{in.TargetVersionID, in.ChannelBindingID} {
		if id != nil {
			if !orderUUID.MatchString(*id) {
				return badOrder("资料引用编号无效")
			}
			*id = strings.ToLower(*id)
		}
	}
	if strings.TrimSpace(in.Instruction) == "" || len(in.Instruction) > 8192 {
		return badOrder("请提供客户原始指令，最多 8192 字节")
	}
	amount, ok := parseOrderAmount(in.SourceAmount, spec.sourceScale)
	if !ok {
		return badOrder("来源金额必须是资产精度内的正数")
	}
	in.SourceAmount = displayOrderAmount(amount, spec.sourceScale)
	if q := in.Quote; q != nil {
		// Rates are recorded with the supplied precision. No confirmation or fee is inferred.
		before, after, _ := strings.Cut(q.Rate, ".")
		if len(q.Rate) > 39 || !plainDecimal.MatchString(q.Rate) || len(before)+len(after) > 38 || len(after) > 18 || strings.Trim(q.Rate, "0.") == "" {
			return badOrder("报价汇率须为正十进制数，最多 38 位有效数字、18 位小数")
		}
		dest, ok := parseOrderAmount(q.DestinationAmount, spec.destinationScale)
		if !ok || q.ExpiresAt.IsZero() || strings.TrimSpace(q.Evidence) == "" || len(q.Evidence) > 2048 {
			return badOrder("请提供有效的目标金额、报价有效期及报价依据")
		}
		q.DestinationAmount = displayOrderAmount(dest, spec.destinationScale)
		q.ExpiresAt = q.ExpiresAt.UTC()
	}
	return nil
}
func (s *Server) instructionAEAD() (cipher.AEAD, error) {
	block, err := aes.NewCipher(s.Key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCMWithRandomNonce(block)
}
func orderFieldContext(orderID string, version int64, field string) []byte {
	return []byte(fmt.Sprintf("kaloq-order:v1:%s:%d:%s", orderID, version, field))
}
func (s *Server) registerOrderRoutes(mux *http.ServeMux) {
	const base = "/api/v1/merchants/{id}/orders"
	mux.HandleFunc("GET "+base, s.auth(s.listOrders))
	mux.HandleFunc("POST "+base, s.auth(s.saveOrder))
	mux.HandleFunc("GET "+base+"/{order_id}", s.auth(s.getOrder))
	mux.HandleFunc("POST "+base+"/{order_id}/versions", s.auth(s.saveOrder))
}
func (s *Server) saveOrder(w http.ResponseWriter, r *http.Request, u User) {
	if !merchantAccess(w, u) {
		return
	}
	merchantID, routeID := r.PathValue("id"), r.PathValue("order_id")
	if !orderUUID.MatchString(merchantID) || (routeID != "" && !orderUUID.MatchString(routeID)) {
		writeOrderError(w, pgx.ErrNoRows)
		return
	}
	var in orderInput
	if !decode(w, r, &in) {
		return
	}
	if err := in.validate(routeID != ""); err != nil {
		writeOrderError(w, err)
		return
	}
	encoded, err := json.Marshal(in)
	if err != nil {
		writeOrderError(w, err)
		return
	}
	mac := hmac.New(sha256.New, s.Key)
	mac.Write([]byte("order-request-v1:" + strings.ToLower(routeID) + ":"))
	mac.Write(encoded)
	digest := mac.Sum(nil)
	ctx := r.Context()
	var id string
	var version int64
	replay := false
	err = s.withTx(ctx, func(tx pgx.Tx) error {
		if e := recheck(ctx, tx, u); e != nil {
			return e
		}
		var profile int64
		var merchantStatus string
		// Serializes mutations for this merchant and validates the pinned profile against the same row.
		if e := tx.QueryRow(ctx, `SELECT row_version,service_status FROM merchants WHERE id=$1 FOR NO KEY UPDATE`, merchantID).Scan(&profile, &merchantStatus); e != nil {
			return e
		}
		var priorDigest []byte
		var priorActor string
		e := tx.QueryRow(ctx, `SELECT order_id,version,actor_id,request_digest FROM order_versions WHERE merchant_id=$1 AND request_id=$2`, merchantID, in.RequestID).Scan(&id, &version, &priorActor, &priorDigest)
		if e == nil {
			if priorActor != u.ID || !hmac.Equal(priorDigest, digest) {
				return &orderProblem{409, "idempotency_conflict", "该请求编号已关联其他内容"}
			}
			replay = true
			return nil
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return e
		}
		if merchantStatus == "paused" {
			return &orderProblem{409, "merchant_paused", "商户服务已暂停"}
		}
		if profile != in.ProfileVersion {
			return &orderProblem{409, "profile_conflict", "商户资料已更新，请重新核对"}
		}
		id = strings.ToLower(routeID)
		version = 1
		if id == "" {
			if in.FiatAsset != s.intakeFiatAsset() {
				return &orderProblem{409, "currency_conflict", "业务法币配置已变化，请重新核对"}
			}
			id = newID()
			number := "O-" + strings.ToUpper(strings.ReplaceAll(id, "-", ""))
			_, e = tx.Exec(ctx, `INSERT INTO business_orders(id,merchant_id,order_number,direction,current_version,created_by) VALUES($1,$2,$3,$4,1,$5)`, id, merchantID, number, in.Direction, u.ID)
		} else {
			var storedDirection, storedFiat string
			if e = tx.QueryRow(ctx, `SELECT o.current_version,o.direction,CASE WHEN o.direction='fiat_to_crypto' THEN v.source_asset ELSE v.destination_asset END FROM business_orders o JOIN order_versions v ON v.order_id=o.id AND v.version=o.current_version WHERE o.merchant_id=$1 AND o.id=$2`, merchantID, id).Scan(&version, &storedDirection, &storedFiat); e != nil {
				return e
			}
			if storedFiat != in.FiatAsset {
				return badOrder("修订时必须保留业务单原法币")
			}
			if storedDirection != in.Direction {
				return badOrder("修订时不能改变业务方向")
			}
			if version != in.ExpectedVersion {
				return &orderProblem{409, "version_conflict", "业务单已更新，请重新核对当前版本"}
			}
			version++
			_, e = tx.Exec(ctx, `UPDATE business_orders SET current_version=$2 WHERE id=$1`, id, version)
		}
		if e != nil {
			return e
		}
		return s.recordVersion(ctx, tx, u, merchantID, id, version, in, digest)
	})
	if err != nil {
		writeOrderError(w, err)
		return
	}
	status := http.StatusCreated
	if replay {
		status = http.StatusOK
	}
	s.respondOrder(w, r, merchantID, id, version, status)
}

type observedOrderBinding struct{ kind, instanceID string }

func checkOrderReferences(ctx context.Context, tx pgx.Tx, merchantID string, in orderInput) (binding observedOrderBinding, err error) {
	spec := in.assets()
	if in.TargetVersionID != nil {
		var targetID, kind, asset string
		var version int64
		err = tx.QueryRow(ctx, `SELECT target_id,version,kind,asset FROM payment_target_versions WHERE merchant_id=$1 AND id=$2`, merchantID, *in.TargetVersionID).Scan(&targetID, &version, &kind, &asset)
		if errors.Is(err, pgx.ErrNoRows) {
			return binding, badOrder("目标版本不属于当前商户或不存在")
		}
		if err != nil {
			return binding, err
		}
		// Appending a target revision takes this same lock, so the selected version
		// stays current through commit. Historical reads do not need this gate.
		if err = lockTargetVersions(ctx, tx, targetID); err != nil {
			return binding, err
		}
		var latest int64
		if err = tx.QueryRow(ctx, `SELECT version FROM payment_target_versions WHERE merchant_id=$1 AND target_id=$2 ORDER BY version DESC LIMIT 1`, merchantID, targetID).Scan(&latest); err != nil {
			return binding, err
		}
		if latest != version {
			return binding, &orderProblem{409, "target_version_conflict", "收款目标版本已更新，请刷新资料后重新核对"}
		}
		if asset != spec.destination || (in.Direction == "fiat_to_crypto" && kind != "chain") || (in.Direction == "crypto_to_fiat" && kind != "bank") {
			return binding, badOrder("收款目标类型或币种与业务方向不符")
		}
	}
	if in.ChannelBindingID != nil {
		var asset string
		err = tx.QueryRow(ctx, `SELECT kind,asset,instance_id FROM provider_bindings WHERE merchant_id=$1 AND id=$2`, merchantID, *in.ChannelBindingID).Scan(&binding.kind, &asset, &binding.instanceID)
		if errors.Is(err, pgx.ErrNoRows) {
			return binding, badOrder("通道资料不属于当前商户或不存在")
		}
		if err != nil {
			return binding, err
		}
		if binding.kind != "enrollment" && !(binding.kind == "virtual_account" && in.Direction == "fiat_to_crypto" && asset == in.FiatAsset) {
			return binding, badOrder("所选通道资料与收款方向或币种不符")
		}
	}
	return binding, nil
}
func holdFor(in orderInput) (string, string) {
	switch {
	case in.TargetVersionID == nil:
		return "缺少商户收款目标版本", "登记收款目标后修订业务单"
	case in.ChannelBindingID == nil:
		return "缺少商户通道开户或收款账户依据", "核对通道资料后修订业务单"
	case in.Quote == nil:
		return "尚未录入报价版本", "取得报价后修订业务单"
	default:
		return "收款目标与通道证据待核验", "核对客户确认、通道资格及报价有效期"
	}
}
func (s *Server) recordVersion(ctx context.Context, tx pgx.Tx, u User, merchantID, id string, version int64, in orderInput, digest []byte) error {
	binding, err := checkOrderReferences(ctx, tx, merchantID, in)
	if err != nil {
		return err
	}
	aead, err := s.instructionAEAD()
	if err != nil {
		return err
	}
	spec := in.assets()
	minor, _ := parseOrderAmount(in.SourceAmount, spec.sourceScale)
	instruction := aead.Seal(nil, nil, []byte(in.Instruction), orderFieldContext(id, version, "instruction"))
	hold, next := holdFor(in)
	_, err = tx.Exec(ctx, `INSERT INTO order_versions(order_id,merchant_id,version,actor_id,request_id,request_digest,profile_version,target_version_id,channel_binding_id,instruction_enc,source_minor,source_asset,source_scale,destination_asset,hold_reason,next_action) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::text::numeric,$12,$13,$14,$15,$16)`, id, merchantID, version, u.ID, in.RequestID, digest, in.ProfileVersion, in.TargetVersionID, in.ChannelBindingID, instruction, minor, spec.source, spec.sourceScale, spec.destination, hold, next)
	if err != nil {
		return err
	}
	if q := in.Quote; q != nil {
		dest, _ := parseOrderAmount(q.DestinationAmount, spec.destinationScale)
		evidence := aead.Seal(nil, nil, []byte(q.Evidence), orderFieldContext(id, version, "quote"))
		_, err = tx.Exec(ctx, `INSERT INTO order_quote_versions(order_id,version,rate,destination_minor,destination_scale,expires_at,evidence_enc) VALUES($1,$2,$3::text::numeric,$4::text::numeric,$5,$6,$7)`, id, version, q.Rate, dest, spec.destinationScale, q.ExpiresAt, evidence)
		if err != nil {
			return err
		}
	}
	if err = recordOrderPipeline(ctx, tx, merchantID, id, version, in, binding, hold); err != nil {
		return err
	}
	action := "order.created"
	if version > 1 {
		action = "order.revised"
	}
	return audit(ctx, tx, u.ID, action, id+"/v"+strconv.FormatInt(version, 10))
}
