# 正式版员工工作台

独立 React / TypeScript / Vite 应用。依赖和锁文件仅属于本目录，不引用根目录 Demo 代码。

后续 UI 设计请先读 [官网 UI 参考](../../docs/design/kaloq-official-ui-reference.md)。官网实测的米白、深森林绿、少量黄绿点缀及标题/正文层次是品牌参考；支付页面的布局、字体大小和交互参数仍需结合内部员工流程、可访问性及真实设备验证。文档中的建议不自动成为业务规则。

从仓库根目录运行：

```sh
python3 v1.0/scripts/with-toolchain.py npm --prefix v1.0/apps/web ci --ignore-scripts --workspaces=false
python3 v1.0/scripts/with-toolchain.py npm --prefix v1.0/apps/web run dev
python3 v1.0/scripts/with-toolchain.py npm --prefix v1.0/apps/web run build
```

开发入口 `http://127.0.0.1:5174`；API 代理默认到 `http://127.0.0.1:4201`，可用 `KALOQ_API_TARGET` 设置。后端允许的 Origin 应与浏览器实际入口一致。

发布路径用 `KALOQ_WEB_BASE=/kaloq-v1/` 或 `npm run build -- --base=/kaloq-v1/` 指定。API 默认跟随 `import.meta.env.BASE_URL`，即 `/kaloq-v1/api/v1`；反向代理映射到后端 `/api/v1`。独立测试服务发布和 Cookie Path 由上级发布脚本管理。

## Day 2 入口

- 密码验证后进入受限动态码验证或首次绑定流程；二维码在本机浏览器内生成，不调用外部二维码服务。
- 恢复码仅在绑定成功后展示一次；密码验证后使用恢复码重新绑定。页面不将凭证写入 localStorage、URL 或日志。
- 运营：商户搜索、状态筛选、创建档案、编辑资料和服务工作台。搜索和筛选在服务端执行，当前最多展示 200 条；尚未实现分页。
- Root：员工创建、固定角色配置和启停。敏感修改需当前动态码；不能在页面授予 Root 或修改自身角色。
- 个人安全：WebAuthn 凭证登记、可用性验证与撤销。此验证仅证明凭证可用，不是资金审批签名。
- 应用识别服务端会话失效并清除页面身份；窗口重新获得焦点和可见页面每分钟复核会话。最终权限始终由服务端检查。

订单、通道开户、资金和对账在商户工作台显示“尚未开放”。所有列表和结果来自 API，不含演示成功数据。

WebAuthn 编解码依据 [W3C WebAuthn](https://www.w3.org/TR/webauthn-3/)，手动转换 base64url 以兼容尚无 JSON 辅助方法的浏览器。真实电脑、手机和恢复场景的验证记录以 `../../docs/day-02/` 为准；构建通过不代表完成真实设备验收。

## 已上线页面的视觉基线

2026-11-05 根据[官网 UI 参考](../../docs/design/kaloq-official-ui-reference.md)更新现有登录页与内部工作台：暖米白背景、深森林绿区域、局部黄绿强调；主按钮为深绿胶囊，表单输入为 48px，卡片圆角约 20px。状态、业务权限与资金开放规则没有因此改变。当前以文字字标显示 `kaloq.`，待取得获准的原始 Logo/字体文件后替换，不依赖官网图片代理。

[视觉核验](../../docs/design/evidence/current-workspace/checks.json)使用受控虚构数据检查 320、375、390、768、1280px；截图为界面参考，不代表真实商户数据。
