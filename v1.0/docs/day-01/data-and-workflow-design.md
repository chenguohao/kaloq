# D1 数据模型与流程设计初稿

日期为 2026-11-04，时区 Asia/Shanghai。本文供 D1–D3 评审，尚未建立正式业务表、执行迁移或验证通道闭环。模型名和 API 路径为建议契约。已纳入当天新增确认：正式法币业务使用 USD，HKD 仅用于 Demo/沙盒样例；角色人员、EO 门槛及测试款采用可更新配置；签名覆盖电脑和手机；平台收费列入后续单独需求。

依据为 [开发约束](../development-baseline.md)、[内部作业需求](../internal-operations-requirements.md)、[缺口与规则](../internal-v1-gap-and-plan.md)、[对账与执行完整性](../reconciliation-and-execution-integrity.md)。既有实现参考 [原数据库模型](../../../apps/api/internal/store/migrations/001_init.sql)。正式迁移的唯一来源为 `v1.0/database/migrations/`。

## 1. D1 设计边界与确认项

- 以内部员工办理商户业务为入口，员工身份和商户身份分开。运营可查看全部商户，审批详情按具体员工隔离。正式版不建立商户自助登录。
- 正式数据库从独立环境起步。原测试版商户成员关系、数字钱包配对、单一 Provider 状态及 Demo JSON 不直接成为正式模型。
- 原 `internal-v1-gap-and-plan.md` 9.2 的“商户首期建议可自助启用”与内部版范围冲突，本稿按内部版范围设计员工 2FA，D1 已同步修订该旧句。
- 固定财务→主管→按门槛 EO、拒绝后结束旧版本、自动执行、同单禁止自审及跨级重复审批均作为待确认策略设计。实现保存策略版本。角色人员可随时由获授权人员更新配置，开工阶段无需指定实际姓名；每笔申请办理时解析并固定指定审批人，人员缺失的节点等待配置，保留明确卡点。
- 正式法币业务使用 USD，HKD 仅用于 Demo/沙盒样例。数币资产/网络和通道资格以联调证据确认。EO 门槛和测试款规则采用可更新的版本配置，具体数值在实际启用相应动作前设置；报价有效期、恢复授权和其他待定策略继续登记。
- 个人凭证签名支持电脑和手机，Google Authenticator 用于身份验证。优先以跨设备 WebAuthn 凭证方案在 D2 验证浏览器、域名/RP ID、签署、恢复与撤销；硬件安全密钥采购不作为开工先决条件。
- 平台收费本期不实现，后续单独提出需求。通道实际扣费作为回执及资金核对事实记录，业务指令按已授权来源扣款总额控制，不新增平台服务费计费引擎。

## 2. 身份、版本与类型约定

平台实体使用独立生成的 UUID，接口当作不透明字符串。商户展示编号另设唯一字段，所有供应商标识存放在绑定或外部流水中。客户端传入的 merchant_id、instance_id 和目标 ID 均须服务端交叉校验。

金额建议统一使用 `numeric(38,0)` 保存最小单位整数，避免未来精度改变时重写资金列。应用使用精确整数并检查 38 位范围，HTTP 与任务载荷使用十进制字符串。报价率使用十进制字符串及明确的舍入规则版本。资产注册后 scale 不可原位改变，需新资产版本和明确转换。USD 与 USDT 的精度沿用现有参考，开放前依据所选资产核实。`asset_id` 区分法币与具体网络资产，网络名称和代币合约属于资产定义，不把不同链同名 USDT 当作同一资金位置。

时间列为 `timestamptz`，存储与事件序列化使用 UTC。上游发生、接收、入账三个时间分别保存。可变聚合根设置 `row_version` 用于条件更新，不可变资料、报价、审批快照用独立版本行。供应商专有 JSON 携带 `schema_version` 并在适配器内验证，关键身份、金额、关联和状态有独立列及约束。

## 3. 实体、关键字段与约束

所有表都有主键和创建时间，下表只列业务必要字段。敏感资料列按受控加密存储，附件和完整目标仅在授权用途下解密。

### 3.1 员工、商户与资料

| 实体 | 关键字段 | 关系与约束 |
|---|---|---|
| staff_users | login_name、password_hash、status、auth_epoch | login_name 唯一，员工启停和恢复提高 auth_epoch，使旧会话失效 |
| staff_role_assignments | staff_id、role、assignment_version、effective_at、revoked_at、grant_event_id | 固定角色代码，具体人员可更新；授权变更追加版本与审计，历史审批保留原指派 |
| staff_sessions | token_hash、staff_id、auth_epoch、assurance、expires_at | 仅完整 2FA 会话可访问业务；挑战会话单独受限 |
| auth_factors / recovery_requests | staff_id、factor_type、encrypted_material 或 public_key_version、state；恢复请求的申请/核验/授权/执行人 | 因子秘密不进审计正文；恢复码哈希一次性消费；恢复授权配置未定前不开放重置 |
| merchants | merchant_code、legal_name、service_status、created_by、row_version | merchant_code 唯一，服务状态与供应商开户状态独立 |
| merchant_profile_versions | merchant_id、version、payload_enc、confirmation_evidence_id、review_state | UNIQUE(merchant_id, version)，已提交版本不可覆盖 |
| documents | merchant_id、purpose、storage_ref、content_hash、classification、retention_state | 查看附件须验证用途及关联审批权限，链接不能绕过本人审批隔离 |
| payment_targets / target_versions | merchant_id、target_kind、version、asset/network、details_enc、confirmation_evidence_id、review_state | UNIQUE(target_id, version)，目标归属不可改商户；生效版本需客户确认与内部审核 |

`auth_factors` 为逻辑模块，TOTP、恢复码和个人签名凭证应分别落实密钥生命周期，不共用一个可以互相替换的 secret 字段。个人签名凭证的完整结构见 3.4。

### 3.2 服务商、账户与资金位置

| 实体 | 关键字段 | 关系与约束 |
|---|---|---|
| provider_instances | provider_code、instance_code、environment、credential_ref、status、active_config_version | UNIQUE(environment, instance_code)，品牌代码不做仅 Pyvio/Actyve 的数据库枚举；实例身份不可变 |
| provider_config_versions | instance_id、version、adapter_version、capability_config、verified_evidence_ref、activated_at | UNIQUE(instance_id, version)，能力包括动作、资产/网络、目标类型、额度和使用范围；配置版本追加 |
| provider_enrollments | merchant_id、instance_id、profile_version_id、external_customer_ref、state、requirements_version | 同一商户可有多实例开户；按供应商外部客户号实际作用域建立唯一键 |
| provider_bindings | instance_id、resource_type、external_scope、external_id、merchant_id 或 pool_owner、schema_version、metadata_enc | UNIQUE(instance_id, resource_type, external_scope, external_id)，scope 统一规范化，不能使用可重指向外部资源的别名 |
| logical_accounts | merchant_id、asset_id、purpose、status | 代表平台记录的商户权益；本期商户长期产品余额为 USD，数币过渡权益按订单追踪 |
| fund_locations | instance_id、account_binding_id、asset_id、location_kind、status | 引用实际钱包/Unit/地址及资产，资产含网络；共享主池作为明确 location，不设全局虚拟总池 |
| collection_methods | merchant_id、location_id、binding_id、method_type、details_version、state | 旧 VA/地址长期绑定原实例；更换收款方式新增记录并保留历史 |
| target_provider_bindings | target_version_id、instance_id、payee_binding_id、verification_state、last_verified_at | 收款目标同一版本可绑定不同实例的受益人；该 payee 解析出的完整目标须能核对 |
| asset_definitions | code、kind、network、contract_ref、scale、version、status | 资产唯一标识覆盖网络/合约，scale 固定；法币采用明确的无网络值 |

绑定的归属为“商户资源”或“平台池资源”二选一，CHECK 防止两者同时为空或同时存在。共享资金位置的访问通过商户头寸与批准步骤授权，不把共享账户绑定到某一个商户。商户级目标、资料和订单通过组合外键或事务内强制校验保持同商户归属。多实例资源通过包含 instance_id 的组合唯一键和外键，防止 A 实例步骤误用 B 的账户。

### 3.3 业务订单与执行

| 实体 | 关键字段 | 关系与约束 |
|---|---|---|
| business_orders | merchant_id、business_number、direction、operator_id、stage、hold_reason、row_version | business_number 唯一；stage 为业务进度，资金与审批状态单列 |
| order_versions | order_id、version、instruction_document_id、profile_version_id、target_version_id、quote_version_id | UNIQUE(order_id, version)，客户原始指令和各资料版本固定 |
| quote_versions | order_id、version、pricing_direction、source/destination_asset、authorized_source_debit、destination_amount、rate、expires_at、customer_confirmation_id | 记录通道报价与客户确认的兑换指令；本期无平台服务费报价、费率表或计费流程 |
| execution_plans | order_version_id、version、route_snapshot、policy_version_id、schema_version、state | 固定逐步资金路径和审批分组，同一计划下的下一步由实际证据满足后启用 |
| execution_steps | plan_id、ordinal、action、instance_id、config_version_id、source_location_id、destination_location_id/target_version_id、amounts、request_id、state、row_version | UNIQUE(plan_id, ordinal)、UNIQUE(instance_id, request_id)，原实例和请求号长期固定；跨平台步骤记录来源和目标归属 |
| step_dependencies | step_id、depends_on_step_id、required_evidence_kind | 依赖无环，业务核验条件独立于仅 HTTP 成功 |
| execution_attempts | step_id、attempt_no、lease_generation、request_digest、adapter_version、transport_result、sent_at、response_evidence_id | UNIQUE(step_id, attempt_no)，每次尝试记录保留；重试仍使用步骤原 request_id |
| external_operations | instance_id、resource_scope、operation_type、external_id、step_id、observation_state | UNIQUE(instance_id, resource_scope, operation_type, external_id)；对只在特定账户唯一的单号保留 scope |
| customer_confirmations | merchant_id、order_id、step_id、kind、document_id、recorded_by、confirmed_at | 区分报价、目标、测试到账、主款到账，保存原始凭证 |

授权扣款采用 quote_debit_lines / approved_debit_lines，逐条固定 asset_id、source_location_id、authorized_max_amount 和通道报价证据，金额为最小单位字符串。每个币种、每个资金位置分别校验和预占；通道另扣网络费资产时同样纳入已批准的扣款明细，缺报价或扣款资产权限则阻断。该明细记录通道资金指令，本期不计算平台收费。

目标到账金额和通道实际扣费按回执保存结构化核对明细，包含币种、金额及外部证据，避免不同币种混算。测试付款以独立 step/申请关联主款，配置记录测试金额、触发条件、`included_in_total` 与相应处理规则，申请快照保存配置版本、授权总额和已测试额。修改配置供新申请使用；在途申请保留原版本，若需改变原单授权金额则重新审批。

### 3.4 个人审批与不可变签名

| 实体 | 关键字段 | 关系与约束 |
|---|---|---|
| approval_policy_versions | policy_code、version、effective_at、rules、approved_by、evidence_ref | 规则含顺序、角色到人员配置版本、EO 门槛及等值口径；更新追加版本，历史申请保留原版本 |
| test_payment_policy_versions | policy_code、version、effective_at、trigger_rules、amount_by_asset、included_in_total、approved_by | 测试款规则可配置，新申请采用生效版本；旧单快照保留具体规则及金额 |
| approval_requests | order_version_id、plan_id、snapshot_id、policy_version_id、submitted_by、state | 每组资金指令一个版本化申请；明确覆盖哪些 execution_steps |
| approval_snapshots | schema_version、canonicalization_version、payload_enc、hash_algorithm、snapshot_hash | 只追加；覆盖完整目标、实例、来源位置、报价/授权来源扣款总额、测试规则和每个授权子步骤 |
| approval_nodes | request_id、ordinal、designated_staff_id、role、state、row_version | UNIQUE(request_id, ordinal)，状态变化条件更新；固定本人查看/办理 |
| signing_credentials | staff_id、credential_id、public_key_version、public_key、credential_type、status、enrollment_evidence | credential_id 唯一，旧公钥版本保留；撤销后未执行申请暂停复核 |
| signing_challenges | node_id、snapshot_hash、staff_id、decision、nonce_hash、challenge_context、expires_at、consumed_at | 绑定环境、用途、人员、节点及决定；一次性消费 |
| approval_decisions | node_id、actor_id、decision、comment_enc、credential_version_id、signature_evidence、signed_at | 同有效节点至多一份决定；签名验证与消费挑战、推进节点、写消息在同事务内 |
| notifications | recipient_staff_id、source_event_id、kind、resource_id、read_at | UNIQUE(recipient_staff_id, source_event_id, kind)，本人消息和本人数量按服务端筛选 |

执行前除历史签名和配置版本外，重查实例即时暂停/资格、审批员工启停、相关角色或权限撤销以及签名凭证撤销。收紧控制使相关已批准但尚未发送的动作暂停复核；旧快照保留证据不构成绕过当前停付的依据。

人员配置更新不删除旧指派、旧决定和签名。已创建节点的转交须执行独立授权操作并留痕、撤销旧挑战和通知原/新指派人；影响已签授权内容时重新生成申请版本。禁用人员立即停止其新操作，历史证据仍可核查。

跨设备 WebAuthn 为 D2 待验证候选，验收需覆盖电脑和手机上的凭证登记与签署。signature_evidence 保存验证所需的 challenge 上下文、clientDataJSON、authenticatorData、signature、credential ID、RP/origin 约束及算法版本。密码/2FA 验证与个人签名分别执行；签名必须关联审批人持有的凭证。后续审批人只得到前置条件满足与业务资料，不得到别人的审批意见、签名包或个人审批记录。

### 3.5 冻结、在途、账本与对账

| 实体 | 关键字段 | 关系与约束 |
|---|---|---|
| ownership_positions | owner_kind、owner_id、location_id、asset_id、bucket、amount、row_version | 唯一键覆盖归属、位置、资产和桶；owner_kind 包含 merchant/platform/unclaimed，归属明确 |
| reservations | request_id、step_id、merchant_id、position_id、purpose、amount、state | 按每条已授权资产/资金位置扣款明细原子预占，活跃占用唯一；通道实扣与授权不符进入差异处理 |
| transit_lots | order_id、step_id、merchant_id、source_location_id、destination_location_id、source_asset/amount、expected_destination_asset、state、source_evidence_id、destination_evidence_id | 来源已扣与目标到账分别取证，非同币种金额不直接相减平账 |
| journal_transactions / entries | posting_key、business_reason、order_id、step_id、reversal_of；account_id、asset_id、delta | posting_key 唯一，每个 journal 按资产平衡；追加更正并关联原记录 |
| pool_balances / pool_movements | location_id、asset_id、balance_basis、amount、sequence；sequence、before、in、out、after、journal_id、actor、executor、prev_digest、digest | UNIQUE(location_id, asset_id, balance_basis, sequence)，池序号与前后值通过同一余额行锁连续分配 |
| provider_observations | instance_id、location_id、asset_id、balance_basis、observed_at、amount、evidence_ref | 上游余额/流水独立保存，不能直接覆盖账面余额 |
| reconciliation_runs / differences | scope、as_of、evidence_cutoff；location/merchant/order、expected、observed、difference_type、owner、state | 保留核对时间窗口、在途及差异证据；关闭差异与解锁执行分开授权 |

`ownership_positions` 表示池资产的归属分析，账务平衡由 journal 控制，二者在同一个记账事务内更新。商户、平台自有和待认领归属合计等于同位置同口径池账面值；冻结是头寸内部用途变化，不再扣一次实际池资产。跨平台已扣资金转入明确的在途科目，目标到账后以对应资产完成分录。

journal 按资产平衡不能只靠单行 CHECK 实现。D3 需选定受控记账入口与提交时校验方式，配合数据库权限拒绝任意绕过写分录；D4 验证任一写入失败整体回滚、固定顺序加锁和并发超额阻断。

### 3.6 事件、任务、审计与告警

| 实体 | 关键字段 | 关系与约束 |
|---|---|---|
| webhook_inbox | instance_id、event_scope、external_event_id、schema_version、body_hash、evidence_ref、received_at、processed_at | UNIQUE(instance_id, event_scope, external_event_id)，签名验证后入库；同事件标识不同正文形成异常证据 |
| business_posting_claims | instance_id、resource_scope、external_operation_id、posting_action、asset_id、event_index | 业务过账独立去重，防不同通知 ID 重复描述同笔到账 |
| outbox_events | aggregate_type/id/version、event_type、schema_version、payload、event_id | 与业务事务同提交，发送与消费均幂等 |
| tasks | task_kind、instance_id、step_id、payload_version、dedupe_key、run_at、state、attempts、lease_owner、lease_until、lease_generation | dedupe_key 唯一，按实例公平领取；旧租约不得更新新代任务结果 |
| audit_events / security_incidents | actor、action、resource、trace_id、evidence_ref、previous_digest；severity、scope、hold_id、assigned_staff、state | 追加审计和独立留存点；安全事件视图单独授权 |
| execution_holds | scope_type/id、reason、created_by、released_by、release_evidence、state | 分开阻断新提交、已有未发送执行和后续步骤；保留查单与对账能力 |

供应商缺少稳定事件 ID 时，按其文档和实测定义语义事件键并保存版本；仅正文哈希不能代替业务过账去重。链上事件键必须含网络、代币标识、交易哈希和 log/output 索引。

## 4. 状态机与事务边界

### 4.1 业务进度

```mermaid
flowchart LR
  A[待资料] --> B[待入金]
  B --> C[待核验]
  C --> D[待兑换]
  D --> E[兑换处理中]
  E --> F[待付款]
  F --> G[付款处理中]
  G --> H[待客户确认]
  H --> I[待对账]
  I --> J[已结单]
```

各阶段另外记录 `hold_reason` 和未满足条件。流程允许按业务方向省略不适用阶段，测试付款为独立关联步骤。Pyvio 出款成功只完成来源证据，须核对 Actyve 目标到账才能满足后续付款条件；A2P 到主账户后，分发到商户子账户证据满足才增加该位置可用。

### 4.2 资金申请与执行

| 对象 | 转换 | 守卫及事务要求 |
|---|---|---|
| 资金申请 | draft → awaiting_approval | 固定版本和路径、核验目标归属/报价/策略，按资金位置锁定并预占、写首节点和通知 outbox，同事务提交 |
| 审批节点 | waiting → accepted/rejected | 本人、2FA/签名与当前版本校验，消费挑战、决定、下一节点/通知一次性提交 |
| 资金申请 | awaiting_approval → approved | 所有必要节点接受且快照一致，可靠落库执行任务；已批准仍需发送前检查 |
| 资金申请 | rejected/expired/withdrawn | 旧版本停止推进；释放范围依据未发送/未扣款证据和已批准策略，修改后建立新版本 |
| 执行步骤 | planned → ready | 前序到账、测试确认、全部审批、能力和资金条件满足 |
| 执行步骤 | ready → dispatching | 任务租约、步骤版本、防重、必要审计、最终请求与签名核验通过，保存不可变业务请求摘要与尝试后提交事务 |
| 执行步骤 | dispatching → processing | 上游明确受理，保存外部关联；不等同到账成功 |
| 执行步骤 | dispatching/processing → unknown | 超时、断连或发送边界崩溃，保留占用/在途，原实例查单 |
| 执行步骤 | processing/unknown → succeeded | 真实终态及该动作所需流水证据通过状态机，执行唯一过账 |
| 执行步骤 | processing/unknown → failed | 明确失败且核实扣款/退回/费用后处理可释放部分 |
| 执行步骤 | ready → blocked | 快照、签名、凭证、目标、报价、余额或暂停条件不满足，记录理由与适用告警 |
| 业务订单 | 待对账 → 已结单 | 全部应完成步骤终结、无未知/冻结/在途/未解决差异，客户确认和剩余处理符合批准规则 |

部分成功由步骤和在途批次表达，主单显示已完成部分与当前卡点，不把整个业务判失败后恢复全部可用。终态后到达冲突事件须进入差异调查，不能静默回退状态。

### 4.3 发送边界与重启恢复

数据库事务不能覆盖外部付款。Worker 按 lease_generation 领取任务，在发送前固定业务请求并复核暂停及凭证状态。网络调用在本地事务外执行，完成后以步骤版本和租约代次条件推进。

`dispatching` 阶段崩溃后，将发送结果视为未知并查原实例。即使查到 `not_found`，也必须依据供应商已验证的查询一致性、受理窗口和幂等协议判断能否用原 request_id 恢复；不得因租约过期再生成付款号。缺少可靠恢复协议则保持人工核实。暂停动作生效于尚未开始的后续发送边界，已发出的请求以原单查询结果处置。

## 5. API 与事件契约草案

以下内部 API 以 `/internal/v1` 为建议命名空间，D2 实现时固定。旧 Demo 路由不注册在正式执行服务。浏览器只提交平台 ID 和业务字段，服务端根据已批准路由选择实例，返回解释性能力状态。

| 操作 | 建议接口 | 关键约束 |
|---|---|---|
| 商户列表/建档 | GET/POST /internal/v1/merchants | 完整员工会话及权限；POST 使用 Idempotency-Key |
| 商户工作台 | GET /internal/v1/merchants/{id} | 商户、开户资格、订单进度与本地/上游资金观测分开展示 |
| 资料与收款目标 | POST /internal/v1/merchants/{id}/profile-versions 或 targets/{target}/versions | 修改生成新版本，校验目标商户归属 |
| 创建/查看订单 | POST /internal/v1/merchants/{id}/orders；GET /internal/v1/orders/{id} | 原始指令、版本、Pipeline、卡点和允许动作；不泄露他人审批记录 |
| 提交资金申请 | POST /internal/v1/orders/{id}/funding-requests | quote_version、target_version、expected_order_version，冻结与申请原子提交 |
| 本人审批 | GET /internal/v1/me/approvals；GET /internal/v1/me/approvals/{id} | 列表/数量/搜索/详情/附件/导出按 designated_staff_id 限定 |
| 签署与决定 | POST /internal/v1/me/approvals/{id}/challenges；POST /internal/v1/me/approvals/{id}/decisions | challenge 绑定快照及决定，业务接口不接收一个 approved=true 直接放行 |
| 查询执行 | GET /internal/v1/orders/{id}/steps | 读取本地记录，补查任务只能查询原实例和原请求号 |
| 对账与异常 | GET /internal/v1/reconciliation/differences；POST /{id}/actions | 处置按权限和证据，冲正需独立授权 |
| 回调 | POST /callbacks/{instance_public_id}/{event_family} | 按实例解析验签配置，URL 参数不是可信身份，验签、资源归属与去重共同确认 |

写操作幂等键作用域为环境、身份/商户、操作和键，保存规范化请求摘要。同键不同请求返回 `IDEMPOTENCY_CONFLICT`，同键同请求返回原资源/结果。用户变更、角色撤销或资源权限变化后仍需授权，幂等命中不能绕过权限。

错误采用稳定平台 code、脱敏 message、trace_id、retryable 和可公开的字段错误。首批 code 建议包含 `CAPABILITY_UNAVAILABLE`、`VERSION_CONFLICT`、`TARGET_OWNERSHIP_MISMATCH`、`APPROVAL_POLICY_INCOMPLETE`、`INSUFFICIENT_AVAILABLE_FUNDS`、`EXECUTION_OUTCOME_UNKNOWN`、`QUOTE_EXPIRED`、`SIGNATURE_INVALID`。供应商原始错误以内部证据关联，客户端不依赖其错误码。

平台事件采用 `{event_id, schema_version, type, occurred_at, recorded_at, aggregate_id, aggregate_version, merchant_id, trace_id, data}`。适配器标准观察另含 instance_id、外部作用域/单号和 evidence_ref，由业务层验证后生成平台事件。消费者以 event_id 去重并校验聚合版本，缺失版本补查，乱序不回退状态。个人审批事件仅投递指定员工，业务进度事件不包含审批人的专属意见。

## 6. 适配接口与配置变更

开户、收款、报价/兑换、付款、查单/对账为独立能力接口。注册表按 provider_instance 解析适配器、凭证引用、配置版本和能力验证记录。业务核心依赖平台 DTO，供应商字段转换位于各适配器。

付款适配器需要有可校验的请求准备阶段，产出完整业务字段投影和请求摘要。执行器与签名快照逐字段对比后，使用同一不可变内容发送。签名头、令牌、时间戳可更新，金额、目标、来源账户和授权扣款范围必须保持已校验值。

仅通过 payee ID 付款且该资源可被上游静默改写时，接入验收需证明可固定版本、不可变目标或发送时足够的目标一致性保证；查询后再提交的时间窗口必须分析。无法满足时，该能力保持阻断并记录上游缺口。

实例配置的 enable_new_submissions、pause_dispatch、allow_lookup、allow_callbacks、allow_reconciliation 分别控制。启用 B 只影响随后批准选择 B 的新业务。已存在 A 的资源、订单、请求号、任务和事件继续由 A 处理。按实例设置并发预算、队列配额、HTTP 超时和熔断恢复，避免 B 故障耗尽 A 的资源。

## 7. 数据库隔离与演进评审清单

1. 正式开发、正式测试与已有 Demo 使用独立数据库名及数据库角色，连接目标与环境启动检查相符。测试命令要求显式专用测试库，不能读取旧 Demo 默认连接。
2. 迁移文件保存在 `v1.0/database/migrations/`，版本表保存版本、校验和、应用版本与执行时间。D2 选定迁移工具，单独迁移命令获取数据库级锁，应用启动不竞相迁移。
3. 首次建库也通过迁移执行。Demo JSON 和历史资料迁入需要映射计划、演练、记录数/归属/金额核验与用户确认，独立于正式业务建表。
4. 上线后的扩展先加兼容结构，后发布新代码，再分批回填和核对。破坏性收缩另开变更，要求旧版本及在途任务已经退出使用。
5. 发布前验证旧应用可读取扩展后数据库，任务 payload_version 可兼容，迁移锁等待与执行时间受控。备份恢复和业务回滚分别演练，不能用旧备份覆盖发布后资金记录。
6. 数据库/应用回滚边界、回调路由和登录会话隔离必须随正式发布配置确认。当前旧服务的部署脚本不能作为正式版来源验证证据。

## 8. D3 前需完成的评审与后续证据

| 评审项 | D1 输出 | 后续完成标准 |
|---|---|---|
| 实体与归属 | 本稿字段及关系 | D3 组合外键、唯一范围、金额范围及索引形成 SQL 草案，业务参数有确认记录 |
| 状态与资金 | 主单、审批、执行、在途分离 | D4–D6 并发冻结、重复事件、部分成功和未知结果验证 |
| 签名和恢复 | 固定版本、挑战、凭证生命周期，电脑/手机支持要求 | D2 跨设备 WebAuthn 与 RP 可行性，D6 最终请求一致性及重放/撤销阻断 |
| 接口 | 平台资源、错误和事件草案 | D3 OpenAPI 与字段映射，旧任务/事件兼容用例 |
| 通道扩展 | 实例、绑定、位置、原单归属 | D13–D14 A/B 双实例新增、故障、停用、重启和默认配置变化回归 |
| 发布与迁移 | 独立库、单一迁移来源和扩展顺序 | D11 锁/耗时、兼容应用回滚、备份恢复和资金记录核对 |

技术评审需重点确认共享资金位置归属约束、资产精度上限、组合外键设计、记账提交时平衡校验、执行租约与未知结果恢复、签名挑战载体及独立审计存储。本文提供初稿，最终 SQL、公开 API 和审批参数以通过评审的后续版本为准。
