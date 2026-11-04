# Kaloq V1 历史入口

> 正式内部版开发资料已迁入 [1.0 目录](../1.0/README.md)，后续开发统一从该入口阅读。下文保留早期商户版与实现历史，不作为当前内部版开发入口。

本目录保留早期 V1 背景资料与旧链接。

**实施前必读：[开工准备与开发约束](development-baseline.md)**。新增同类服务商不得要求停机重整存量数据；多实例绑定、数据库兼容演进、历史订单归属、故障隔离及验收门槛纳入本轮必做底座。此文是开发约束，不代表已实现。

内部作业流程见 [内部版需求整理稿](internal-operations-requirements.md)（2026-10-31，依据业务流程截图整理，含角色分工、双向兑换流程、审批、对账及待确认事项；尚未作为已实现功能）。

第一版开发拆解见 [内部版第一版：现有基础、开发缺口与规则草案](internal-v1-gap-and-plan.md)，包含 Demo 复用范围以及审批、冻结和执行的具体方案。

可视化范围见 [第一版功能总览（分层功能树）](internal-v1-function-tree.md)，提供完整 Mermaid 源码与可缩放总图。

内部员工后台的页面规划见 [内部版页面清单](internal-v1-page-inventory.md)：以全部商户列表为运营首页，进入商户后办理服务，建议 35 种页面模板；商户通过群沟通，由运营代录，本期不做商户登录和自助操作。

页面跳转见 [内部版 Mermaid 页面关系图](internal-v1-page-map.md)，包含全部 35 页；商户页内展示订单列表，订单详情展示 Pipeline、当前环节、卡点原因和处理入口。

开发安排见 [内部版 15 天冲刺计划](internal-v1-15-day-plan.md)：以沙盒业务闭环为交付目标，列明每日成果、范围取舍、通道依赖和验收节点。

新增独立系统见 [总池对账、审批签名与监控告警](reconciliation-and-execution-integrity.md)，包含逐笔原子流水、个人签名快照、最终请求防篡改及主管/EO 告警。

先读本页的简版范围，再读 [第一版功能清单](feature-list.md) 和 [法币与 U 的资金路径](fund-flow.md)。[详细需求与架构](detailed-requirements.md) 里 2026-10-08 的页面表还写着 USDT 钱包和自有划转，那些不作为第一版。根目录更早的研究笔记仅作背景；遇到范围冲突，以本目录里的功能清单和资金路径为准。

## 一眼看懂第一版

完整的 Feature List、Function List 在 [feature-list.md](feature-list.md)。

| 编号 | Feature |
|---|---|
| F1 | 开户 |
| F2 | 收美元（银行 VA） |
| F3 | 提到自己的银行卡 |
| F4 | 付给他人银行卡 |
| F5 | 付 U（美元余额打到链上地址） |
| F6 | 收 U（临时地址，到账后变回美元） |
| F7 | 余额与记录 |
| F8 | 成员与权限 |
| F9 | 接入 API |

不做商户自己的 USDT 钱包，也不做「划转 Convert」。

## 边界

- 首期商户：一家乌兹别克斯坦企业；首期通过 Pyvio 以 **USD** 作为法币切入（收美元、USD 法币钱包），数字资产暂定 USDT。商户所在地不代表首期收款币种是 UZS。
- 首期通道：法币在 Pyvio，U 的进出经过 Actyve 总池。商户长期余额只在 Pyvio 美元钱包。路径、接口和已确认的兑换点见 [法币与 U 的资金路径](fund-flow.md)。KYC/KYB 仍由 Pyvio 执行。平台负责统一 API、工作台、交易状态和运营账本。
- Sent 表示向外部目标付款/转账。美元提到自己的银行卡走 Pyvio 提现（Withdraw），不经过 Actyve。出 U 不是商户自己的第二个钱包，而是 Pyvio 美元经 P2A 进入 Actyve 总池，再提到已审核的链上地址。
- Pyvio 公开文档没有商户级 USDT 钱包。不要再按「每个 VA 配对一个 Pyvio 数字账户」设计。
- 用户资料快照仅在已授权的用途内加密保留和复用。资料过期或主体信息变化时要求重新确认。
- 不假设任意币种、网络、第三方目标或四条资金路径都已获 Pyvio 开通；实现前用通道能力矩阵确认，未确认的能力不得显示为可用。
- 对外 API 隔离 Pyvio 字段和 ID，按稳定业务资源建模；增加 Provider 时不要求客户修改现有 API。
- 网页前端只调用 Kaloq API；Kaloq 服务端通过 Provider Adapter 调 Pyvio。网页发布 Kaloq 自己的 API 文档，包含认证、请求/响应、错误、幂等、回调和示例，不把 Pyvio 文档当作我们的 API 文档。

## 建议交给编码代理的阅读顺序

1. 本页：产品范围和硬边界。
2. [第一版功能清单](feature-list.md)：Feature List 与 Function List。
3. [法币与 U 的资金路径](fund-flow.md)：收美元、提现、付 U、收 U 怎么走。
4. [安全审计与风控方案](security.md)：全流程风险点；按事前、事中、事后和三类风险组织的风控、对账，以及治理和应急。合规另由合规同事负责。
5. [详细需求与架构](detailed-requirements.md)：较早的页面和架构笔记。与功能清单冲突时，以功能清单为准。
6. [通用程序架构](../program-architecture.md)：理解 Provider 抽象与模块边界。

编码代理开始实现前，先按功能清单改页面和接口。未在沙盒确认的通道能力保持未开通，不要在页面上显示为可用。

## 当前实现状态（2026-10-08）

V1 的平台契约、Go 后端、模拟通道（Fake Provider）和中文商户工作台已经可运行，运行方式和页面说明见根目录 [README](../../README.md)。已用后端集成测试和浏览器完整走通：开户（含附件上传与授权）→ 沙盒入金 → Sent 法转法 → 自有账户 USD→USDT 划转 → 记录与账本分录 → 创建 API 凭证并验证权限边界 → API 文档页。

| 项目 | 状态 |
|---|---|
| Kaloq API（含 OpenAPI、幂等、事件与 Webhook、权限） | 已实现，接口文档由路由表自动生成并发布在 `/docs` |
| 账本、报价、状态机、结果未知只查询不重复提交 | 已实现并有测试 |
| 模拟通道 | 已实现，页面顶部有常驻提示 |
| Pyvio Adapter（`provider/pyvio.go`） | **占位**：所有调用返回"未实现"，能力矩阵全部为"未开通"。拿到沙盒资料后再实现 |
| 运营对账 / 人工处理工具 | 未做（上线前需要） |

### Pyvio 能力缺口清单（必须在沙盒确认后才能开放对应功能）

1. **USD 收款路径**：乌兹别克斯坦商户能否通过 Pyvio 开 USD VA；VA 的真实字段（SWIFT、账号、附言/reference 规则）。
2. **VA 与数字账户配对**：2026-10-12 作废。Pyvio 不给商户单开 USDT 钱包，见 [资金路径](fund-flow.md)。
3. **数字资产网络与币种**：改到 Actyve。出 U 前要 Chain Payee 审核；收 U 用 Acquire 临时地址。链、地址格式、最小金额仍待沙盒确认。
4. **自有账户兑换**：2026-10-12 作废。美元与 U 的转换发生在 P2A（到账 USDT）和 A2P（到账 USD），不是商户自己的两个 Pyvio 钱包互转。
5. **Sent 两条路径**：法转法仍走 Pyvio 银行付款。出 U 改走 P2A 加 Actyve 提现，不再假设 Pyvio 有数转数接口。
6. **KYC/KYB 资料**：必填字段与附件清单；附件 `file_id` 的有效期；已通过的 client 能否在更新资料时复用；补件（打回）的回调格式；资料过期的处理方式。
7. **Webhook**：签名算法、事件 ID 是否稳定唯一、是否会乱序和重复、重试策略。
8. **查询接口**：交易状态查询的幂等键/外部引用字段，用于"结果未知"时的只读确认。

这些确认前，对应路径在能力矩阵 `GET /v1/capabilities` 中保持"未开通"，页面不得显示为可用。

### 已拍板：机构模式（2026-10-09）

Kaloq 以 **Pyvio 机构模式**接入，不用大商户模式。

- Kaloq 自己是机构账号（APPID / Secret / 签名密钥属于这一层）。
- 每个商户单独做 KYC，对应 Pyvio 里的一个子商户（Unit）。VA 和钱包开在这个商户名下，钱不进 Kaloq 自己的账户。
- 操作某个商户时，请求头带该商户的 `X-Unit-Id`。
- B2B 的 User 可以申请 1 个 VA；B2C 必须先提交店铺，提交后立即拿到 `shopID`（不用等店铺审核完），申请 VA 时要带上它，一个 User 最多 10 个 VA。VA 即 Pyvio 的 Global Account。会上细节见 [资金路径](fund-flow.md) 的「2026-10-13 会议」。机构自己的主 Unit 仍要确认，以后收分成用。

### 沙盒探测结果（2026-10-08，使用 Pyvio 文档页提供的测试账号，只做只读查询）

- **鉴权已验证**：`POST /oauth/api/v1/token`（`grant_type=client_credentials`）加 RSA 签名（`appId + timestamp + body`，SHA256WithRSA，PKCS8）可以换到 JWT；GET 请求带 `Authorization` 与 `Request-Id` 即可。令牌响应里有 `access_token` 和 `refresh_token`。
- **测试账号现状**：Unit 状态 Active（名称 Developer），只有一个 USD 钱包，余额 0；VA 列表为空。
- **公开文档里没有任何 USDT/稳定币接口**（余额、VA、转账、兑换都只涉及法币）。2026-10-12 已与对方确认：U 不在 Pyvio 里给商户单开钱包，而走 Actyve 总池。见 [法币与 U 的资金路径](fund-flow.md)。
- 文档中可对接的法币接口：开户 `/user/api/v2/user/register`、开 VA `/user/api/v1/va/apply`、余额 `/finance/api/v1/balance`、汇率与换汇 `/fx/api/v1/*`、付款 `/outbound/api/v1/transfer/*`、收款明细 `/inbound/api/v1/collect/*`。
- 测试账号的 Pyvio 公钥（用于验证回调签名）和回调地址尚未拿到。
