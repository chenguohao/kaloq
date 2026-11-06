# Kaloq 正式版 1.0 开发区域

本目录用于接下来的正式版开发。已发布 Day3 商户业务单、资料与收款目标版本、双向订单进度。操作和验证见[第三天执行记录](docs/day-03/README.md)。此前记录见[第二天身份与商户入口](docs/day-02/README.md)和[第一天准备](docs/day-01/README.md)。

| 目录 | 用途 |
|---|---|
| apps/api/ | 正式后端，独立 Go 模块与依赖 |
| apps/web/ | 正式前端，独立 package.json 与依赖 |
| database/migrations/ | 正式版数据库版本与迁移脚本 |
| scripts/ | 开发工具链、检查与正式沙盒发布脚本 |
| [docs/](docs/README.md) | 正式开发需求、约束、排期及验收资料 |

## 与既有工程的边界

- 仓库根目录 apps/、scripts/ 和 package.json 属于既有测试版与 Demo。保留原位作为参考和维持现有服务；其启动、构建与发布命令不代表正式版命令。
- 新业务代码默认在本目录编写。复用旧代码时，先核对需求、依赖及资金控制，迁入后通过正式版检查；不能直接运行或导入旧业务实现作为正式执行入口。
- 不复制旧 node_modules、构建产物、数据库数据、演示账号、环境文件或凭证。供应商密钥继续存放于仓库根 secrets/，正式配置仅引用凭证，不复制进本目录。
- 正式开发和测试数据库必须与既有 Demo 分开，使用独立数据库与迁移版本记录；禁止正式迁移脚本连接 Demo 数据库。正式模型不以兼容旧 Demo 表结构为前提，但正式上线后必须遵守兼容演进约束。
- 前后端使用独立清单和锁文件，禁止默认沿用根目录 npm workspace。新增依赖和初始化命令在本目录文档中记录。
- 在正式版发布配置完成前，不使用旧 Demo 脚本发布本目录。需部署到已授权的独立 Demo 测试服务时，明确发布来源、备份和回滚后更新专用脚本，继续遵守根 AGENTS.md；不得覆盖其他站点。
- docs/ 中待确认事项保持待确认，正式代码按开发约束和验收门槛落实。

先读 [开发资料入口](docs/README.md) 和 [开发约束](docs/development-baseline.md)。

## 开发机工具链

Go 和 Node 版本及官方校验和锁定在 toolchain.json。运行方式与验证记录见 [D1 环境记录](docs/day-01/environment-and-checks.md)。

## 当前可用入口

- 正式沙盒：[Kaloq 1.0](https://v-2077.com/kaloq-v1/)。运营可进入商户工作台登记指令、创建业务单、查看卡点和修订历史版本。
- [API 开发说明](apps/api/README.md) · [前端开发说明](apps/web/README.md) · [部署与验收](docs/day-03/README.md)。
- 后端独立依赖为 apps/api/go.mod；前端为 apps/web/package.json 与 package-lock.json；数据库 SQL 唯一来源为 database/migrations/。
- 开发检查：提供隔离的 TEST_DATABASE_URL 后运行 `bash scripts/check.sh`；测试库须以 `_test` 结尾，不能使用运行库。
- 正式沙盒发布：`bash scripts/deploy-sandbox.sh`。该脚本只构建本目录，并使用独立 kaloq-v1 服务和数据库；旧 Demo 发布脚本继续只用于旧代码。
- 真实设备签名仍待验收，资金业务动作保持未开放，详见 D2 记录。
