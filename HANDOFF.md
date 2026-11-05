# Meta

- Updated: 2026-11-05 12:07:25 (Asia/Shanghai)
- Agent: Codex
- State: working

# Goal

交付 Day2 正式应用、员工身份/2FA/恢复、商户入口和个人签名技术验证，发布正式沙盒并登记待验收事项。

# Done

- 正式代码已写入 v1.0/apps/api、apps/web，独立依赖和构建入口；身份/商户迁移唯一来源为 v1.0/database/migrations。
- 实现密码与强制 TOTP、受限挑战、一次性恢复码、限流、员工角色/启停、历史授权和会话撤销；Root 无隐含商户或资金权限。
- 实现平台独立商户 ID、建档、搜索、详情、联系人加密、资料版本更新与并发冲突控制。
- WebAuthn 登记、验签试验与撤销已实现；挑战绑定员工/会话/用途/版本，恢复后旧凭证失效，保留历史。
- 隔离 PostgreSQL 下十项后端测试（含 race）、vet、前端构建通过。Chrome 九项全流程通过，含虚拟凭证登记/验签、恢复失效与手机宽度布局；会话撤销后清空界面的专项检查通过。
- 正式沙盒已发布 https://v-2077.com/kaloq-v1/ ，release 20261105T040121Z。独立服务 kaloq-v1 / 4102、独立数据库 kaloq_v1_sandbox；迁移和运行身份分离。旧 kaloq-demo 仍 active 且健康。
- 公网实际密码、TOTP、完整会话、权限限制、签名登记挑战和受限恢复检查通过；初始 owner 已回到首次绑定状态，无活动会话，未留测试认证器。
- 发布前数据库快照已保存；发布脚本验证 Nginx，启动/路由失败可回切应用。首次发布成功不等同 D11 完整备份恢复演练。
- 两笔 Day2 成果已本地提交；本轮没有 push。

# In progress

Day2 核心实现与测试发布已完成，等待真实设备和业务验收；不能将整个 Day2 外部依赖计为已关闭。临时本地测试服务/数据库已关闭。正式订单、资金表和通道写执行未实现。

# Blocked/questions

- 真实电脑/手机认证器与跨端流程待本人设备验收；已有 P256 软件测试和 Chrome 虚拟认证器通过。
- 人工无恢复码重置、Root 更换和细化管理授权待确认，相关入口保持关闭。初次绑定必须保管恢复码。
- 实际开发投入、业务验收与通道联系人仍待补充。角色人员和阈值仍由配置维护，不写死人员。
- USD 权限、数币网络、P2A 对端到账、反向路径和银行出款仍缺证据，未开放相关资金动作。
- 此前 Slash 调研未证实客户资金模型与 on-ramp 能力，仍需书面确认；未据此替换现有通道。
- 当前两个沙盒同源不同路径不是浏览器安全隔离。生产需独立 origin/RP ID，变更域名须重新登记凭证；当前资金功能关闭。

# Next

1. 业务方按 v1.0/docs/day-02/README.md 首次绑定 owner、创建运营账号，走查商户与真实设备凭证。
2. 落实人工恢复授权和通道证据，记录实际可开放范围。
3. D3 评审并实现业务单、阶段、Pipeline、资料/报价版本与实例绑定；资金执行按 D6 门槛。
4. 继续本地提交；获得新的 push 指令后再推送。

# Evidence

- Day2 implementation: 3af72ef82f1b68e77a71c8b9f659413e0ec5dd16
- Day2 deploy/evidence/docs: 2d9cb03 (chore: deploy isolated formal sandbox and record Day 2 acceptance)
- Day1 pushed baseline: aaac74e14ca79348b0a7992886e49389c3660e91
- 本交接自身提交可由 git log -1 -- HANDOFF.md 定位。
- v1.0/docs/day-02/README.md
- v1.0/apps/api/check-results.json
- v1.0/docs/day-02/evidence/browser-result.json
- v1.0/docs/day-02/evidence/session-expiry.json
- v1.0/docs/day-02/evidence/remote-login.json
- v1.0/docs/day-02/evidence/public-browser.json
- v1.0/docs/day-02/evidence/remote-status.txt
- v1.0/scripts/deploy-sandbox.sh
- v1.0/deploy/runtime-grants.sql
- https://v-2077.com/kaloq-v1/
- Prior Slash research: https://docs.slash.com/api-reference/transfer-crypto-offramp ; https://api.slash.com/openapi ; https://docs.slash.com/openapi-v2.json
