# Pyvio / Actyve 官方文档研究笔记

> 研究日期：2026-09-20  
> 只根据公开文档整理，不替代官网。接口字段以官方页面为准。

官方入口：

- Pyvio API：https://developer-doc.pyvio.com
- Pyvio 说明：https://docs.pyvio.com
- Actyve API：https://developer-doc.actyve.io

---

## 1. 先看结论

| | Pyvio（湃沃） | Actyve |
|--|---------------|--------|
| 一句话 | 新兴市场法币全球收付 | 传统支付 + 链上账本 |
| 钱的形态 | 各国法币 VA + 钱包 | 链上 USDT/USDC 地址 |
| 典型入金 | 银行打款、平台结算款、银行卡收银台 | 链上转入（另有 FundTransfer） |
| 典型出金 | 付给银行/钱包、结汇、虚拟卡消费 | 提到链上、银行账户、或 GlobalAccount |
| 中文说明站 | 有（docs.pyvio.com） | 公开站几乎只有 API |
| 文档风格 | VuePress 式 API + 中文指南 | 同一套模板，示例密钥都相同 |

两家很像「一套底层、两个品牌」（**2026-10-03 再核公开材料**）：

- 签名都是 `appId + timestamp + requestBody`，SHA256WithRSA，PKCS8 / 2048，结果 Base64 放进 `Sign`
- 响应都是 `{ "code": "SUCCESS", "data": {} }`，错误码 `B_XXXX` / `S_XXXX`
- API 文档里的示例 app_id、RSA 密钥字符串一样
- Actyve 提现 `order_type` 含 `GlobalAccount`，目标填 **wallet id**（示例 `uxehvym7oedko3vysum65okzxzqrsinx`）
- Actyve 账单接口字段原文把 `unit_id` / `client_id` 写成 *identifier … in the **Pyvio** platform*
- 两边开发者文档的构建记录里都是 `@pyvio.cn` 的人（如 `fanlixing@pyvio.cn`）

**仍不能写成「资金已打通」：** Pyvio 公开站不出现 Actyve / USDT；两套 Host、两套 `app_id`；没有公开联调步骤，也没有沙箱实测「U 出金 → Pyvio USD 钱包余额增加」。

对接时仍按 **两套 Host、两套密钥、两套 webhook** 处理。

---

## 2. Pyvio 说明文档（https://docs.pyvio.com）

首页三块产品：

1. **全球本地账户**：USD / HKD / EUR / GBP / JPY 等本地收款账户  
2. **全球支付**：付给供应商、结汇提现  
3. **湃付卡 PyCard**：虚拟卡，多币种结算

顶部栏目：指南、收款、付款、换汇、收单、湃付卡、其它、更新日志。

### 2.1 账户体系

来源：https://docs.pyvio.com/pages/guid/user/

| 名词 | 含义 |
|------|------|
| Client | 一个实名主体。KYC 通过后默认带一个 Unit |
| Unit | 业务单元。钱、VA、入账、付款都在这里。可开多个做隔离 |
| VA / GA | Global Virtual Account。只收款，不存钱 |
| 钱包 | 每个 Unit 每个币种一个。同币种 VA 入账汇总进该钱包。付款用 `origin_currency` 指定从哪个钱包出 |

多 Unit：商户后台创建，或调「创建子商户」。KYC 通过后才能用 `client_id`。

### 2.2 对接模式

来源：https://docs.pyvio.com/pages/guid/mode/

| 模式 | 怎么用 |
|------|--------|
| 机构模式 | 我们替下面的子商户报数（Create User）。操作子商户时请求头必须带 `X-Unit-Id`。VA 开在子商户名下 |
| 大商户模式 | 令牌只操作自己的 Unit。`X-Unit-Id` 可不传 |

具体用哪种，文档要求问 BD。

**2026-10-03 商务口头**：机构合作会另开提价/分成后台；公开 API 无 `customer_fee` 字段。详见 [pricing.md](pricing.md) 第 4 节。

### 2.3 开户

来源：https://docs.pyvio.com/pages/user/path/

同步返回几乎一定是 `Pending`（已有通过 KYC 的 `client_id` 时可能直接 `Active`）。后续靠 User Notification：

| 状态 | 含义 |
|------|------|
| Pending | 待审 |
| Active | 通过 |
| Returned | 打回，用 Update User 改资料；补材料走 `file_type=ED` |
| Suspend / Deact | 合规不可用，不能再开 |
| PreOverdue | 材料快过期，功能仍可用 |

KYC 被打回或拒绝时，该主体下所有 Unit 变 `Pending`，API 不能用。

### 2.4 收款

来源：https://docs.pyvio.com/pages/collection/ 等

| 类型 | 场景 | 限制 |
|------|------|------|
| B2C / Collection | 电商平台结算款 | 先创建店铺拿 `shop_id`，再开 VA；一个 shop_id 只能用一次 |
| B2B Collection | 海外买家银行回款 | 个人不能用；必须填贸易类型 |
| Direct Payment | 账户充值 | 每国每币种最多 1 个 VA |

B2C 和 B2B 若要同时做，必须开不同类型的 Unit。

开 VA 接口示例：`POST /user/api/v1/va/apply`  
额度：Collection 每国每币种最多 10 个；Direct Payment / B2B Collection 各 1 个。

入账：https://docs.pyvio.com/pages/collection/inbound/

- `Pending` 时钱在「待结算账户」（`account_type=2`）
- 不一定要补材料；需要时走附件提交 + 审核通知
- **入账是否成功，不取决于材料审没审过**
- 终态 `Success` / `Failure` 后不要再改状态

B2C 常要平台出款证明；B2B 常要合同、银行水单、境外物流单。

### 2.5 付款

来源：https://docs.pyvio.com/pages/payment/create/

- `order_type`：`Withdraw`（提现）或 `Payout`（付款）
- 支持错币种，不必先换汇
- `payee_id` 和 `payee` 二选一；也可按卡号/SWIFT 等匹配已有收款人，匹配不到就新建
- `cal_origin`：`ORIGIN2TARGET` 或 `TARGET2ORIGIN`
- `charge_indicator`：`OUR` / `SHA`
- `pobo`：`Y` / `N`
- 付到人民币且带 B2B 标签时，要合同订单清单

### 2.6 换汇

来源：https://docs.pyvio.com/pages/fx/

- 文档写换汇不收手续费
- 单笔询价锁汇约 **60 秒**（`rate_id`）
- 只看参考价可用批量询价
- 支持币种对会变，以生产为准。文档列出 USD/EUR/HKD/CNH 与 VND、IDR、MYR、PHP、NGN、MXN、KRW、THB 等

### 2.7 结汇

来源：https://docs.pyvio.com/pages/repatriation/

付给人民币要外汇申报。额度来自审核通过的订单金额。B2B 订单要填贸易类型、买卖双方、物流等。额度在主商户/子商户间默认不隔离，要隔离得自己管。

### 2.8 收单收银台

来源：https://docs.pyvio.com/pages/acquirer/cashier/

商户拿 token → 前端跳转 Pyvio 收银台 → 买家填卡 → 可能 3D 验证 → 终态 webhook。商户要回 `OK`。另有服务端支付、退款、捕获、撤销。

### 2.9 湃付卡

来源：https://docs.pyvio.com/pages/issuing/create/ 与 API 目录

流程：加持卡人 → 开卡（`card_bin_id` 问 BD，开卡币种文档写死 USD）→ 异步通知或查询 → 充值 / 提现 / 冻卡 / 限额 / 流水。

---

## 3. Pyvio API（https://developer-doc.pyvio.com）

Host：

- Sandbox `https://sandbox-api.pyvio.com`
- Production `https://api.pyvio.com`

公共头：`Request-Id`（7 天内不重复）、`Authorization`（JWT）、POST 还要 `Sign`、`X-Timestamp`、`Content-Type: application/json`。子商户要 `X-Unit-Id`。

有 Java / PHP SDK。鉴权是 Client Credentials。

侧边栏模块（按业务）：

| 模块 | 接口大意 |
|------|----------|
| User | 创建 / 查询 / 更新用户，用户通知 |
| Balance | 余额、余额列表、流水 |
| Global Account | 开户、查询、欢迎信、通知 |
| Shop | 店铺 CRUD + 通知 |
| Collection | 入账列表/详情、附件、退款、转入通知 |
| Payee | 收款人 CRUD |
| Order / Contract | 上传订单、合同订单、物流补传（结汇材料） |
| Repatriation | 可结汇额度、结汇通知 |
| FX | 询价、批量询价、换汇下单 |
| Payment | 询价、创建付款、凭证、通知、退款通知 |
| Transfer | 内部转账（Unit 之间） |
| Acquirer | 收银台、S2S 支付、退款、捕获、撤销、物流 |
| Issuing | 持卡人、开卡、限额、余额、充提、冻卡、授权/清算流水 |
| File / Report | 上传、报表下载 |
| Appendix | 国家币种、美国州、印尼省市 |

签名示例规则：`signContent = appId + timestamp + requestBody`。

---

## 4. Actyve API（https://developer-doc.actyve.io）

Host：

- Sandbox `https://sandbox-api.actyve.io`
- Production `https://api.actyve.io`

路径前缀示例：`/actyve/api/v1/...` 与 `/actyve/api/v2/...` 混用。  
介绍页原文：把传统支付和 distributed ledger 合在一起，做全球收付。

公开营销站 `actyve.io` / `actyve.com` 几乎没有产品说明，**能力以这份 API 为准**。

### 4.1 账户与链

`GET /actyve/api/v2/account/getAccount`  
返回多条链地址，例如：

- `chain_name`: Tron / Ether / Solana / Polygon / Base / TON / BNB / Arbitrum
- `chain_address`: 链上收款地址
- `status`: Active / Suspend

`GET /actyve/api/v1/account/getAllBalance`  
按地址列出余额，示例币种 **USDT、USDC**。

Scope：`account_read`。

### 4.2 入金

Deposit Notification 字段：

- `order_type`：`BlockchainTransfer`（链上转入）或 `FundTransfer`
- `amount` / `currency` / `chain_name`
- `chain_address`（我方地址）、`from_address`、`chain_tx`、`deposit_time`

Acquire 模块：创建收单单、查询、入金列表。通知文案：**链上每进一笔，用 Acquire Result Notification 告知合作方**。

### 4.3 出金

`POST /actyve/api/v1/withdraw/create`

| 字段 | 要点 |
|------|------|
| `order_type` | `Chain` / `GlobalAccount` / `BankAccount` |
| `currency` | USDT 或 USDC |
| `chain_name` | 默认 Tron |
| `target_address` | Chain 填链上地址；GlobalAccount 填钱包 id |
| `pobo` | 仅 BankAccount。`Y` 时付款人用你的 KYC 信息，额外收费 |
| `rate_id` | 要换汇时先调 Query FX Rate |

状态：`Processing` / `Success` / `Failure`。链上出金带 `chain_tx`。

### 4.4 换汇

`POST /actyve/api/v2/fx/queryRate`

- 卖出：USDC / USDT  
- 买入：USDC / USDT / USD / HKD  
- 返回 `rate_id`、`rate`、`expire_time`

示例：USDT → USD，rate `0.9985`。

错误码里单独有 `B_Outbound_RateExpiredError`（汇率过期）。

### 4.5 收款人

两套：

- **Payee**：偏法币银行收款人（创建 / 编辑 / 查询 / 删除 + 通知）
- **Chain Payee**：链上收款人（同样一套 CRUD + 通知）

### 4.6 其它

Notification、报表下载、文件上传。附录有美国州、印尼省市（给银行账户地址用）。

鉴权同样是 Client Credentials + Scope（如 `withdraw_write`、`acquire_read`）。

---

## 5. 和旧 BVNK 方案怎么对照

旧 `eopay` 归档里的六块，可以这样重新映射（**还没定做哪些**）：

| 旧模块 | 更接近谁 |
|--------|----------|
| ① 法币 VA | Pyvio Global Account + 钱包 |
| ③ 换汇 | Pyvio 法币换汇；Actyve 稳定币兑 USD/HKD |
| ④ 链上转账 | Actyve 入金 / 链上出金（替代 BVNK） |
| ⑥ 收单 | Pyvio 收银台；Actyve Acquire 是链上入金建单 |
| 虚拟卡 | Pyvio 湃付卡 |
| ⑤ KYC | Pyvio 自带开户；Actyve 文档几乎不讲独立 KYC |

不要把 Dinari / 用币买股票的流程写进本仓库。

---

## 6. 接入时都要准备的东西

两家共通：

1. 找对方 Dev / BD 拿 `app_id`、`app_secret`
2. 生成商户 RSA 密钥对（PKCS8，2048），把公钥给他们
3. 拿到平台公钥，用来验 webhook
4. 报 IP 白名单
5. POST：`Request-Id` + `Sign` + `X-Timestamp`
6. webhook 要验签，并按文档回执（Pyvio 收银台要求回 `OK`）

Pyvio 另要确认：机构模式还是大商户模式、B2B 还是 B2C Unit、店铺平台名单、卡 BIN。  
Actyve 另要确认：开通哪些链、BankAccount 出金是否可用、GlobalAccount 是否就是 Pyvio 账户。

---

## 7. 还没在公开页看细的

- Actyve Create Acquire 的完整请求字段（页面是动态加载，本次只确认了通知语义）
- Actyve Payee / Chain Payee 的字段枚举
- Pyvio 支持的具体国家、平台、卡 BIN 列表（要问 BD）
- Actyve Create Acquire 的完整请求字段（页面是动态加载，本次只确认了通知语义）
- Actyve Payee / Chain Payee 的字段枚举
- Pyvio 支持的具体国家、平台、卡 BIN 列表（要问 BD）
- 两家是否共用商户主体、能否资金互转（文档只暗示 `GlobalAccount` 这个名字）
- **Actyve 公开材料无价目、无分成后台、无 Create User / Transfer**（2026-10-03 核对 JS 全包；详见 [pricing.md](pricing.md) 1.2）
