# Meta

- Updated: 2026-11-05 17:49:24 (Asia/Shanghai)
- Agent: Codex
- State: done

# Goal

下载 Kaloq 官网原始图标并用于正式版现有界面。

# Done

- 已核对官网 HTML 中的 Logo 与 favicon 路径，下载深色、浅色透明 PNG 和 favicon.ico 至 v1.0/apps/web/public/brand/。
- 已查看原图内容，替换工作台文字字标并设置浏览器图标；不同背景使用相应深浅版本。
- TypeScript/Vite 构建通过；320、375、390、768、1280px 浏览器预览均无横向溢出或页面错误。
- 素材、整合与视觉证据已本地提交 7933335。
- 正式沙盒 release 20261105T094815Z 已部署；官网三张资源在线返回 200 且 SHA-256 与本地一致，登录与健康 200，未认证 me 401。

# In progress

- 无，本次素材替换和发布已完成。

# Blocked/questions

- Instrument Serif 字体文件仍未托管；当前系统后备字体保留。真实设备验收另行进行。

# Next

1. 后续页面继续使用本地品牌素材；原始来源和校验值见 public/brand/README.md。
2. 待取得 Instrument Serif 原始字体文件和真实设备验收，再处理字体及设备表现。
3. 本轮本地提交，不 push。

# Evidence

- 官网： https://kaloq.com/ 与 https://kaloq.com/images/kaloq-logo-dark.png、https://kaloq.com/images/kaloq-logo-light.png、https://kaloq.com/favicon.ico。
- v1.0/apps/web/public/brand/
- v1.0/apps/web/src/App.tsx；v1.0/apps/web/index.html。
- 前端 npm run build 成功；v1.0/docs/design/evidence/current-workspace/checks.json；提交 7933335。
- v1.0/apps/web/public/brand/README.md（官网来源和 SHA-256）。
- 正式沙盒 release 20261105T094815Z：3 张资源哈希一致、login/health 200、未登录 me 401。
