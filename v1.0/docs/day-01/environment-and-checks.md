# D1 环境与验证记录

2026-11-04，Asia/Shanghai。所有测试结果限定于执行对象，原型和旧版测试均不代替正式资金业务验收。

## 代码基线

仓库 HEAD 为 `aaa8dc609dca0fb096e9733eb2c7a7b16f792e9f`，包含大量既有未提交代码。已在用户本地备份目录创建源代码与文档归档及逐文件 SHA-256 清单，位置、归档哈希和筛选范围见 [基线记录](evidence/source-baseline.json)。恢复时先解压到独立空目录并核对清单，避免覆盖其他工作。归档不包含环境文件、凭证资料、依赖或构建产物，因此不是数据库/业务运行环境备份。

## 本机检查

| 项目 | 初始情况 | D1 处理 |
|---|---|---|
| Go | go1.22rc1，旧版 Makefile 依赖外部链接规避 macOS 问题 | 先按旧版命令验证，再准备独立稳定工具链 |
| Node/npm | Node 20.16.0 / npm 10.8.1 | 旧版构建退出 0，但 Vite 输出要求 Node 20.19+ 或 22.12+，正式基线换用单独 Node 22 工具链 |
| PostgreSQL | 17 系列工具已安装，默认 5432 未启动 | 建立临时 Unix socket 集群，端口标识 55441，无 TCP 监听；创建 kaloq_d1_test，验证后已关闭 |
| 正式目录 | apps、database/migrations 仍为空骨架 | 先评审模型，D2 初始化独立模块，迁移入口统一到 database/migrations |

临时测试关闭 live probe/OCR 私有材料的环境入口，测试进程显式使用测试库与临时样例目录，并设置 KALOQ_SKIP_ENV_FILE=1。临时库采用本机隔离目录下的 trust 认证，仅用于此次一次性测试，不作为正式部署的认证配置。

已执行的基线命令如下。后端测试实际连接上面临时集群，不使用默认 kaloq_test 或任何 Demo 库。

```sh
# 在仓库根 apps/api 中，使用隔离 TEST_DATABASE_URL 与 GOTOOLCHAIN=local
# 旧编译器的外部链接参数来自现有 Makefile
go test -ldflags=-linkmode=external -count=1 ./...
go vet ./...
# 仓库根，验证既有前端
npm run build -w apps/web
```

三项均退出 0。测试输出涉及 api、dococr、money、sandbox 包通过，其余包无独立测试。显式 opt-in 的 live sandbox 探测和私有 OCR 材料测试未启用。详细命令退出码、耗时和日志见 [检查记录](evidence/baseline-checks.json)、[后端测试](evidence/legacy-go-tests.log) 和 [前端构建](evidence/legacy-web-build.log)。代码审查发现的金额溢出、任务租约和提交回写边界仍需要迁入时补验证，现有测试通过不等于覆盖这些缺口。

## 正式开发工具链

版本、官方下载地址和 SHA-256 锁定于 [toolchain.json](../../toolchain.json)，安装在本用户 `~/.local/share/kaloq/toolchains/`，通过 [命令包装器](../../scripts/with-toolchain.py) 使用。它是 macOS arm64 开发机工具链，服务器构建目标另行配置。

```sh
cd v1.0
python3 scripts/with-toolchain.py go version
python3 scripts/with-toolchain.py node --version
python3 scripts/with-toolchain.py npm --version
```

归档来自 [Go 官方发布清单](https://go.dev/dl/?mode=json) 和 [Node 22 官方校验和](https://nodejs.org/dist/latest-v22.x/SHASUMS256.txt)，下载后核对 SHA-256 再解包。新增机器按锁文件中的精确 URL 下载并核对哈希，将归档内顶层目录置于上述缓存根目录/版本号，之后再运行包装器。更新版本须修改锁文件并重新验证，避免开发中自动漂移。

独立工具链已实测 Go 1.27.3、Node v22.23.4。用该工具链重新执行旧版 Go 测试（无需外部链接参数）、go vet 及前端构建，全部退出 0，Node 版本警告已消除，临时数据库再次关闭。见 [锁定版本复测](evidence/pinned-checks.json)。

## 独立测试服务检查

D1 公网只读核验结果为 healthz 200 且 status=ok，登录页 HTML 200，未认证 v1/me 返回 401。登录页响应仅证明页面可访问，完整密码/2FA/认证登录仍需专项检查。见 [HTTP 核验](evidence/remote-readonly.json)。

SSH 只读核验确认 kaloq-demo 为 active，运行用户 kaloq-demo，工作目录 /opt/kaloq-demo，服务监听 127.0.0.1:4101。仓库专用脚本的源目录仍为根 apps/，因此正式版发布前要明确构建源、数据库、迁移角色、会话与回调命名空间，以及回滚方式。该项是部署评估，未执行服务切换。

正式开发库建议 kaloq_v1_dev，测试库建议 kaloq_v1_test，实际创建时分配独立角色并检查连接目标。当前只创建过本次临时测试集群，正式数据库尚未建立。正式上线前的兼容迁移和恢复验证按 D11 门槛执行。

## 原型检查边界

原型为单文件离线 HTML，全部内容为虚构样例。已进行 JavaScript 语法和轻量 DOM 逻辑检查，覆盖建档、资料不足、订单与申请、个人审批、配置版本和重复提交。可从仓库根运行 `python3 v1.0/scripts/with-toolchain.py node v1.0/docs/day-01/evidence/check-wireframe.cjs` 复查。结果见 [原型检查](evidence/wireframe-check.log) 与 [交付检查汇总](evidence/deliverable-checks.json)。

浏览器安全策略拒绝 file URL，本轮未完成浏览器视觉和真实点击走查。该限制不影响源文件交付；本机直接打开 HTML 后可按“评审说明”走查。D2 页面实现时仍须验证桌面和手机布局、真实服务端权限、2FA 和签名。
