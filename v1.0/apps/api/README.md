# Kaloq 1.0 API — Day 2

独立 Go 模块 `kaloq/v1`。实现员工密码与强制 TOTP、受限绑定挑战、一次性恢复码、员工启停和角色版本、商户建档/修改/搜索、个人 WebAuthn 登记/验证试验。正式迁移的唯一来源是 `../../database/migrations/`，不依赖仓库根 Demo 代码或数据库。

## 命令与配置

从本目录构建：

```sh
python3 ../../scripts/with-toolchain.py go build -o /tmp/kaloq-v1 ./cmd/kaloq
# 先由独立迁移角色运行，MIGRATIONS_DIR 指向正式 SQL 目录
/tmp/kaloq-v1 migrate
# 仅空身份库可执行一次；从秘密环境配置传入初始账户，不输出密码
/tmp/kaloq-v1 bootstrap
/tmp/kaloq-v1 serve
```

程序不读取 `.env`；由进程环境或服务管理器加载配置。

| 环境变量 | 含义 |
|---|---|
| DATABASE_URL | PostgreSQL 连接；数据库名必须以 `kaloq_v1_` 开头 |
| MIGRATIONS_DIR | `migrate` 的唯一 SQL 来源绝对路径 |
| TOTP_ENCRYPTION_KEY | 随机 32 字节密钥的标准 Base64；保护 TOTP 秘密及商户联系人密文，存于 Git/Web 根之外 |
| PUBLIC_ORIGIN | 浏览器来源，例如 `https://v-2077.com`；本地开发允许 HTTP localhost/127.0.0.1，WebAuthn 使用 localhost |
| COOKIE_PATH | 会话路径，独立部署配置 `/kaloq-v1/`；空值默认 `/` |
| WEB_DIR | 可选前端构建目录；静态文件与平台 API 同源提供 |
| LISTEN_ADDR | 默认 `127.0.0.1:4102` |
| TRUST_PROXY_LOOPBACK | 仅值 `true` 且来源是 loopback 时使用 `X-Real-IP` 限流；反向代理必须覆盖该请求头 |
| BOOTSTRAP_LOGIN / BOOTSTRAP_NAME / BOOTSTRAP_PASSWORD | 一次性初始 Root；密码 12–72 字节，不可覆盖或恢复既有账户 |

`migrate` 使用非阻塞 advisory lock、逐文件 SHA-256 和逐迁移事务；校验和改变直接拒绝。事务锁等待最多 5 秒，语句最多 30 秒。运行账户应与迁移账户分离，业务进程不自动迁移。运行时数据库最低授权与独立发布步骤由 `../../deploy/` 及 `../../scripts/` 维护。

## 身份与数据边界

- API 前缀 `/api/v1`，`/healthz` 检查数据库连通性。POST/PATCH 必须为 JSON 且 Origin 与配置一致；完整会话写请求另须 `X-CSRF-Token`（从 `/me` 获取）。
- 密码登录仅产生 5 分钟 `enroll` 或 `totp` 挑战，TOTP 完成后轮换为 8 小时完整会话。会话随机 token 仅以 SHA-256 保存；cookie 为 HttpOnly/SameSite Strict，HTTPS 下 Secure。
- TOTP 为 30 秒、6 位、SHA-1，容许前后一个时间步；员工行锁及递增的已用时间步防并发重放。挑战失败最多 5 次；登录、验证码、恢复与敏感修改还有数据库持久化限流。
- 恢复需要密码挑战及一次性恢复码，撤销全部既有会话/恢复码，提高认证版本并要求重新绑定。旧 Passkey 按版本失效，保留历史。无恢复码的人工重置仍未开放，授权规则待确认。
- 当前默认商户数据权限只给 `operator`；`root` 仅维护员工，必须再次输入未用过的 TOTP，不能编辑自身或在网页授予/修改 Root。其余业务角色先登记，业务数据范围随对应功能核定。
- 商户 UUID/显示编号独立于供应商。联系人 AES-GCM 加密保存，资料按版本追加，修改用 `row_version` 防覆盖；平台服务状态不代表供应商 KYC 或开户结果。
- 本阶段不提供资金写接口。WebAuthn 仅验证员工凭证技术路径；签名不代表批准任何订单。电脑/手机实际设备矩阵仍需验证。
- 角色权限与人工恢复的细化政策待业务核定，现有实现为保守权限边界。初次员工创建凭证须通过受控方式交给本人完成 2FA 绑定。

## 验证

隔离 PostgreSQL 测试库名必须以 `_test` 结尾。测试会清空该库内身份和商户相关表，禁止传入沙盒运行库或其他业务库。

```sh
# TEST_DATABASE_URL 由测试环境提供
python3 ../../scripts/with-toolchain.py go test -race -count=1 -v ./...
python3 ../../scripts/with-toolchain.py go vet ./...
```

2026-11-05：上述两项全部通过，服务端测试耗时约 4.3 秒。覆盖注册/登录挑战隔离、错码与过期、同一 TOTP 并发仅一次成功、CSRF/Origin、恢复码失效与重新绑定、员工停用撤销、Root 无隐含商户权限、自我提权拒绝、联系人密文、资料并发版本、迁移校验和与数据库隔离；WebAuthn 测试覆盖真实 P-256 验签、错误 Origin/RP ID/挑战/用户验证/签名、跨会话和跨员工隔离、重放、6 并发挑战仅一次成功、撤销与恢复失效。

这些检查使用隔离数据和软件签名器。实际浏览器验证、手机设备范围及发布结果由 Day2 执行记录汇总，不以 API 单测替代真实设备或资金验证。

## Day3 业务单

服务启动时读取 `KALOQ_INTAKE_FIAT`，支持 HKD 和 USD，缺省为 USD。新建时由商户资料接口告知前端本环境的受理币种。修订和查询按原订单币种处理。新加坡沙盒的 systemd 服务显式设置 HKD。

商户路径下的订单、开户与 VA 关联、收款目标及版本契约见 [订单 OpenAPI](../../docs/day-03/orders.openapi.yaml) 和[资料 OpenAPI](../../docs/day-03/resources.openapi.yaml)。API 写入沿用完整运营会话、Origin 及 CSRF 校验。
