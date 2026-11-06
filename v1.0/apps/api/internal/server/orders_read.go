package server

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
)

type orderSummary struct {
	ID               string    `json:"id"`
	Number           string    `json:"order_number"`
	MerchantID       string    `json:"merchant_id"`
	Direction        string    `json:"direction"`
	SourceAmount     string    `json:"source_amount"`
	SourceAsset      string    `json:"source_asset"`
	DestinationAsset string    `json:"destination_asset"`
	Status           string    `json:"status"`
	CurrentStage     string    `json:"current_stage"`
	HoldReason       string    `json:"hold_reason"`
	NextAction       string    `json:"next_action"`
	Version          int64     `json:"version"`
	CurrentVersion   int64     `json:"current_version"`
	CreatedAt        time.Time `json:"created_at"`
	VersionCreatedAt time.Time `json:"version_created_at"`
	OperatorName     string    `json:"operator_name"`
	RecordedByName   string    `json:"recorded_by_name"`
	ProfileVersion   int64     `json:"profile_version"`
	TargetVersionID  *string   `json:"target_version_id,omitempty"`
	ChannelBindingID *string   `json:"channel_binding_id,omitempty"`
}
type orderStage struct {
	Code      string    `json:"code"`
	Label     string    `json:"label"`
	Status    string    `json:"status"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"created_at"`
}
type orderStep struct {
	ID         string  `json:"id"`
	Action     string  `json:"action"`
	StageCode  string  `json:"stage_code"`
	Label      string  `json:"label"`
	Status     string  `json:"status"`
	InstanceID *string `json:"instance_id,omitempty"`
	BindingID  *string `json:"binding_id,omitempty"`
}
type orderQuote struct {
	quoteInput
	Version int64 `json:"version"`
	Expired bool  `json:"expired"`
}
type orderDetail struct {
	orderSummary
	PinnedTarget  *resourceTarget  `json:"pinned_target,omitempty"`
	PinnedBinding *resourceBinding `json:"pinned_binding,omitempty"`
	PinnedProfile resourceProfile  `json:"pinned_profile"`
	Instruction   string           `json:"instruction"`
	Quote         *orderQuote      `json:"quote,omitempty"`
	Stages        []orderStage     `json:"stages"`
	Steps         []orderStep      `json:"steps"`
}

const orderColumns = `o.id, o.order_number, o.merchant_id, o.direction,
    v.source_minor::text, v.source_scale, v.source_asset, v.destination_asset,
    phase.status, phase.code,
    CASE WHEN q.expires_at <= now() THEN '报价已过期' ELSE v.hold_reason END,
    CASE WHEN q.expires_at <= now() THEN '取得新报价和客户确认后修订业务单' ELSE v.next_action END,
    v.version, o.current_version, o.created_at, v.created_at,
    creator.display_name, recorder.display_name, v.profile_version, v.target_version_id, v.channel_binding_id`
const orderJoins = ` FROM business_orders o
    JOIN order_versions v ON v.order_id=o.id
    JOIN staff_users creator ON creator.id=o.created_by
    JOIN staff_users recorder ON recorder.id=v.actor_id
    LEFT JOIN order_quote_versions q ON q.order_id=o.id AND q.version=v.version
    JOIN LATERAL (SELECT code,status FROM order_stages
        WHERE order_id=v.order_id AND version=v.version AND status IN ('blocked','pending')
        ORDER BY ordinal LIMIT 1) phase ON true `

func (o *orderSummary) read(row pgx.Row, extra ...any) error {
	var minor string
	var scale int
	dst := []any{&o.ID, &o.Number, &o.MerchantID, &o.Direction, &minor, &scale, &o.SourceAsset, &o.DestinationAsset, &o.Status, &o.CurrentStage, &o.HoldReason, &o.NextAction, &o.Version, &o.CurrentVersion, &o.CreatedAt, &o.VersionCreatedAt, &o.OperatorName, &o.RecordedByName, &o.ProfileVersion, &o.TargetVersionID, &o.ChannelBindingID}
	if err := row.Scan(append(dst, extra...)...); err != nil {
		return err
	}
	o.SourceAmount = displayOrderAmount(minor, scale)
	return nil
}
func (s *Server) listOrders(w http.ResponseWriter, r *http.Request, u User) {
	if !merchantAccess(w, u) {
		return
	}
	mid := r.PathValue("id")
	if !orderUUID.MatchString(mid) {
		writeOrderError(w, pgx.ErrNoRows)
		return
	}
	var offset int
	if raw := r.URL.Query().Get("offset"); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 32)
		if err != nil || n < 0 {
			writeOrderError(w, badOrder("分页位置无效"))
			return
		}
		offset = int(n)
	}
	ctx := r.Context()
	var exists bool
	if err := s.Pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM merchants WHERE id=$1)`, mid).Scan(&exists); err != nil {
		writeOrderError(w, err)
		return
	}
	if !exists {
		writeOrderError(w, pgx.ErrNoRows)
		return
	}
	const pageSize = 100
	rows, err := s.Pool.Query(ctx, `SELECT `+orderColumns+orderJoins+`WHERE o.merchant_id=$1 AND v.version=o.current_version ORDER BY o.created_at DESC,o.id DESC LIMIT $2 OFFSET $3`, mid, pageSize+1, offset)
	if err != nil {
		writeOrderError(w, err)
		return
	}
	defer rows.Close()
	items := make([]orderSummary, 0)
	for rows.Next() {
		var o orderSummary
		if err = o.read(rows); err != nil {
			writeOrderError(w, err)
			return
		}
		items = append(items, o)
	}
	if err = rows.Err(); err != nil {
		writeOrderError(w, err)
		return
	}
	var next *int
	if len(items) > pageSize {
		items = items[:pageSize]
		n := offset + pageSize
		next = &n
	}
	respond(w, 200, map[string]any{"items": items, "next_offset": next})
}
func (s *Server) getOrder(w http.ResponseWriter, r *http.Request, u User) {
	if !merchantAccess(w, u) {
		return
	}
	mid, id := r.PathValue("id"), r.PathValue("order_id")
	if !orderUUID.MatchString(mid) || !orderUUID.MatchString(id) {
		writeOrderError(w, pgx.ErrNoRows)
		return
	}
	var v int64
	if raw := r.URL.Query().Get("version"); raw != "" {
		var err error
		v, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || v < 1 {
			writeOrderError(w, badOrder("业务单版本无效"))
			return
		}
	}
	s.respondOrder(w, r, mid, id, v, 200)
}
func (s *Server) respondOrder(w http.ResponseWriter, r *http.Request, mid, id string, v int64, status int) {
	ctx := r.Context()
	var detail orderDetail
	var instruction []byte
	query := `SELECT ` + orderColumns + `,v.instruction_enc ` + orderJoins + ` WHERE o.merchant_id=$1 AND o.id=$2 AND v.version=CASE WHEN $3::bigint=0 THEN o.current_version ELSE $3 END`
	err := detail.read(s.Pool.QueryRow(ctx, query, mid, id, v), &instruction)
	if err != nil {
		writeOrderError(w, err)
		return
	}
	aead, err := s.instructionAEAD()
	if err != nil {
		writeOrderError(w, err)
		return
	}
	plain, err := aead.Open(nil, nil, instruction, orderFieldContext(detail.ID, detail.Version, "instruction"))
	if err != nil {
		writeOrderError(w, err)
		return
	}
	detail.Instruction = string(plain)
	var q orderQuote
	var evidence []byte
	var dest string
	var scale int
	err = s.Pool.QueryRow(ctx, `SELECT version,rate::text,destination_minor::text,destination_scale,expires_at,evidence_enc FROM order_quote_versions WHERE order_id=$1 AND version=$2`, id, detail.Version).Scan(&q.Version, &q.Rate, &dest, &scale, &q.ExpiresAt, &evidence)
	if err == nil {
		plain, err = aead.Open(nil, nil, evidence, orderFieldContext(detail.ID, detail.Version, "quote"))
		if err != nil {
			writeOrderError(w, err)
			return
		}
		q.Evidence = string(plain)
		q.DestinationAmount = displayOrderAmount(dest, scale)
		q.Expired = !q.ExpiresAt.After(time.Now())
		detail.Quote = &q
	} else if !errors.Is(err, pgx.ErrNoRows) {
		writeOrderError(w, err)
		return
	}
	if err = s.loadPinnedResources(ctx, &detail); err != nil {
		writeOrderError(w, err)
		return
	}
	if err = s.loadOrderPipeline(ctx, &detail); err != nil {
		writeOrderError(w, err)
		return
	}
	respond(w, status, map[string]any{"order": detail})
}
func (s *Server) loadOrderPipeline(ctx context.Context, d *orderDetail) error {
	rows, err := s.Pool.Query(ctx, `SELECT code,label,status,reason FROM order_stages WHERE order_id=$1 AND version=$2 ORDER BY ordinal`, d.ID, d.Version)
	if err != nil {
		return err
	}
	d.Stages, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (orderStage, error) {
		stage := orderStage{CreatedAt: d.VersionCreatedAt}
		err := row.Scan(&stage.Code, &stage.Label, &stage.Status, &stage.Reason)
		if stage.Code == d.CurrentStage {
			stage.Reason = d.HoldReason
		}
		return stage, err
	})
	if err != nil {
		return err
	}
	rows, err = s.Pool.Query(ctx, `SELECT id,stage_code,action,label,status,instance_id,binding_id FROM order_execution_steps WHERE order_id=$1 AND version=$2 ORDER BY ordinal`, d.ID, d.Version)
	if err != nil {
		return err
	}
	d.Steps, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (orderStep, error) {
		var step orderStep
		err := row.Scan(&step.ID, &step.StageCode, &step.Action, &step.Label, &step.Status, &step.InstanceID, &step.BindingID)
		return step, err
	})
	return err
}

func (s *Server) loadPinnedResources(ctx context.Context, d *orderDetail) error {
	if err := s.Pool.QueryRow(ctx, `SELECT version,legal_name,created_at FROM merchant_profile_versions WHERE merchant_id=$1 AND version=$2`, d.MerchantID, d.ProfileVersion).Scan(&d.PinnedProfile.Version, &d.PinnedProfile.LegalName, &d.PinnedProfile.CreatedAt); err != nil {
		return err
	}
	if d.TargetVersionID != nil {
		rows, err := s.Pool.Query(ctx, `SELECT `+targetColumns+` FROM payment_target_versions WHERE merchant_id=$1 AND id=$2`, d.MerchantID, *d.TargetVersionID)
		if err != nil {
			return err
		}
		target, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByPos[resourceTarget])
		if err != nil {
			return err
		}
		d.PinnedTarget = &target
	}
	if d.ChannelBindingID != nil {
		rows, err := s.Pool.Query(ctx, `SELECT `+bindingColumns+` FROM provider_bindings b JOIN provider_instances i ON i.id=b.instance_id WHERE b.merchant_id=$1 AND b.id=$2`, d.MerchantID, *d.ChannelBindingID)
		if err != nil {
			return err
		}
		binding, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByPos[resourceBinding])
		if err != nil {
			return err
		}
		d.PinnedBinding = &binding
	}
	return nil
}
