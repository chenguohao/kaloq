# Kaloq 内部版页面关系图

本图包含页面清单中的全部 35 种页面模板。运营从商户列表进入某商户服务工作台，在商户内看订单列表，进入订单详情查看 Pipeline、当前环节、停留时长、卡点原因及下一步操作。

- 方框为页面；模块大框为分组；“全局导航”为共有导航组件，不单独算页面。
- 实线为主要页面跳转；虚线为全局导航进入模块或安全设置的入口。所有入口按角色授权展示，导航不授予额外数据权限。
- 仅画主要跳转，返回上页、返回商户、通用个人菜单和全部列表筛选状态省略。商户页内订单列表和详情页内 Pipeline 不单独计页。
- 图中的箭头是页面关系，不是付款执行顺序。审批始终由指定员工办理，运营不可通过订单详情打开他人的专属审批。
- 蓝色为商户/订单，紫色为个人审批，金色为资金对账，红色为监控审计，青色为设置维护，灰色为账号安全。颜色表示页面分组，不表示通道或订单状态。

[页面清单及 Pipeline 详细要求](internal-v1-page-inventory.md) · [Mermaid 源文件](diagrams/internal-v1-page-map.mmd) · [可缩放 SVG](diagrams/internal-v1-page-map.svg) · [PNG](diagrams/internal-v1-page-map.png)

```mermaid
---
config:
  layout: elk
  theme: base
  themeVariables:
    fontFamily: Arial, PingFang SC, Microsoft YaHei
    fontSize: 16px
  flowchart:
    curve: linear
    nodeSpacing: 20
    rankSpacing: 32
---
flowchart TB
  subgraph AUTH[登录与个人安全]
    P01[员工登录] --> P02[二次验证]
    P01 --> P03[首次绑定]
    P03 --> P02
    P01 --> P04[账号恢复]
    P05[安全设置]
  end

  subgraph MERCHANT[商户服务]
    P06[商户列表 · 首页] --> P07[创建／编辑商户]
    P06 --> P08[商户工作台<br/>含本商户订单列表]
    P07 --> P08
    P08 --> P09[KYC 资料]
  end

  subgraph ORDER[订单办理]
    P10[全部订单] --> P12[订单详情<br/>Pipeline · 当前环节 · 卡点]
    P11[创建／编辑订单] --> P12
    P12 --> P13[资金申请]
    P13 -->|提交后返回| P12
    P12 --> P14[报价管理]
    P12 --> P15[业务异常]
  end

  subgraph APPROVAL[个人审批与消息]
    P16[我的审批] --> P17[审批详情]
    P18[消息中心] -->|本人审批| P17
  end

  subgraph FUNDS[资金与对账]
    P19[入金核验]
    P20[商户账本] --> P22[冻结与在途]
    P21[总池账本] --> P23[对账工作台]
    P22 --> P23
    P23 --> P24[差异详情]
  end

  subgraph SECURITY[监控与审计]
    P25[监控概览] --> P26[告警中心]
    P26 --> P27[安全事件]
    P27 --> P28[审计日志]
  end

  subgraph ADMIN[设置与维护]
    P29[员工管理] --> P30[角色权限]
    P31[审批路线] --> P32[业务规则]
    P33[通道管理] --> P34[任务与回调]
    P35[安全恢复申请]
  end

  P02 -->|运营登录成功| P06
  P08 -->|发起服务| P11
  P08 -->|点击订单| P12
  P08 -->|核验入金| P19
  P08 -->|查账| P20
  P12 -->|查资金| P22
  P12 -->|结单核验| P23
  P12 -->|仅本人获派审批| P17
  P18 -->|获授权安全通知| P27
  P15 -->|账务差异| P24
  P15 -->|安全异常| P27
  P25 -->|排查任务| P34
  P29 -->|恢复申请| P35

  NAV([全局导航 · 非独立页面])
  P02 --> NAV
  NAV -.-> P05
  NAV -.-> ORDER
  NAV -.-> APPROVAL
  NAV -.-> FUNDS
  NAV -.-> SECURITY
  NAV -.-> ADMIN

  classDef account fill:#f1f5f9,stroke:#94a3b8,color:#334155
  classDef merchant fill:#eff6ff,stroke:#60a5fa,color:#1e40af
  classDef order fill:#dbeafe,stroke:#3b82f6,color:#1e3a8a
  classDef focus fill:#1d4ed8,stroke:#1e40af,color:#fff,stroke-width:3px
  classDef approval fill:#f5f3ff,stroke:#a78bfa,color:#5b21b6
  classDef funds fill:#fffbeb,stroke:#fbbf24,color:#92400e
  classDef security fill:#fff1f2,stroke:#fb7185,color:#9f1239
  classDef admin fill:#f0fdfa,stroke:#2dd4bf,color:#115e59
  class P01,P02,P03,P04,P05,NAV account
  class P06,P07,P08,P09 merchant
  class P10,P11,P13,P14,P15 order
  class P12 focus
  class P16,P17,P18 approval
  class P19,P20,P21,P22,P23,P24 funds
  class P25,P26,P27,P28 security
  class P29,P30,P31,P32,P33,P34,P35 admin
```
