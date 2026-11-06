package server

import (
	"context"
	"github.com/jackc/pgx/v5"
)

type plannedStage struct {
	code, label string
	steps       []plannedStep
}
type plannedStep struct{ action, label string }

func orderPipeline(direction string) []plannedStage {
	stages := []plannedStage{{"intake", "登记业务单与客户指令", nil}, {"materials", "核对资料、目标与报价", nil}}
	if direction == "fiat_to_crypto" {
		stages = append(stages,
			plannedStage{"receipt", "法币入金核验", []plannedStep{{"verify_fiat_receipt", "核对收款账户与入金流水"}}},
			plannedStage{"transfer_approval", "划转与兑换申请审批", nil},
			plannedStage{"convert", "划转并兑换数币", []plannedStep{{"transfer_fiat", "从原资金位置划转法币"}, {"fiat_conversion", "提交法币转数币指令"}}},
			plannedStage{"counterparty_receipt", "核验对端数币到账", []plannedStep{{"verify_crypto_credit", "关联对端实际到账证据"}}},
			plannedStage{"test_payment", "核定测试付款要求", nil},
			plannedStage{"payout_approval", "数币付款申请与审批", nil},
			plannedStage{"payout", "按确认目标出款", []plannedStep{{"crypto_payout", "向已核准链上目标付款"}}},
		)
	} else {
		stages = append(stages,
			plannedStage{"deposit_address", "核对数币收款地址与网络", nil},
			plannedStage{"receipt", "数币入金核验与归集", []plannedStep{{"verify_crypto_receipt", "核对链上实际入账"}, {"collect_crypto", "核对数币归集"}}},
			plannedStage{"transfer_approval", "兑换申请与审批", nil},
			plannedStage{"convert", "兑换并核验主账户到账", []plannedStep{{"crypto_conversion", "提交数币转法币指令"}, {"verify_fiat_credit", "核验对端主账户法币入账"}}},
			plannedStage{"distribution", "核对商户账户分发", []plannedStep{{"allocate_fiat", "分发并核对子账户实际入账"}}},
			plannedStage{"payout_approval", "银行付款申请与审批", nil},
			plannedStage{"payout", "按确认银行目标出款", []plannedStep{{"bank_payout", "向已核准银行账户付款"}}},
		)
	}
	return append(stages, plannedStage{"confirmation", "客户确认到账", nil}, plannedStage{"reconcile", "对账与结单", nil})
}

// Only an identified VA can pin a receipt observation. Enrollment evidence does not authorize an account or route.
func recordOrderPipeline(ctx context.Context, tx pgx.Tx, merchantID, id string, version int64, in orderInput, binding observedOrderBinding, hold string) error {
	stepNo := 0
	for n, stage := range orderPipeline(in.Direction) {
		status, reason := "pending", ""
		switch stage.code {
		case "intake":
			status = "recorded"
		case "materials":
			status, reason = "blocked", hold
		}
		_, err := tx.Exec(ctx, `INSERT INTO order_stages(order_id,version,ordinal,code,label,status,reason) VALUES($1,$2,$3,$4,$5,$6,$7)`, id, version, n+1, stage.code, stage.label, status, reason)
		if err != nil {
			return err
		}
		for _, step := range stage.steps {
			var instanceID, bindingID *string
			if step.action == "verify_fiat_receipt" && binding.kind == "virtual_account" {
				instanceID = &binding.instanceID
				bindingID = in.ChannelBindingID
			}
			stepNo++
			_, err = tx.Exec(ctx, `INSERT INTO order_execution_steps(id,merchant_id,order_id,version,ordinal,stage_code,action,label,status,instance_id,binding_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'planned',$9,$10)`, newID(), merchantID, id, version, stepNo, stage.code, step.action, step.label, instanceID, bindingID)
			if err != nil {
				return err
			}
		}
	}
	return nil
}
