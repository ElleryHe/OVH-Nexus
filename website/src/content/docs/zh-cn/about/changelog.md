---
title: 版本更新日志 (Changelog)
description: OVH Nexus 的版本发布历史与功能演进记录。
---

:::note[当前最新版本：v1.4.0]
您可以在 [GitHub Releases](https://github.com/ElleryHe/OVH-Nexus/releases) 与 [Docker Hub (`elleryhe/ovh-nexus`)](https://hub.docker.com/r/elleryhe/ovh-nexus) 查看所有正式版发布包与多架构容器镜像。
:::

:::tip[RSS 订阅版本更新日志]
支持在任意 RSS 阅读器（Feedly、Inoreader、NetNewsWire、Follow 等）中订阅 OVH Nexus 版本发布通知：**[`/zh-cn/rss.xml`](https://ovh-nexus.ellery.me/zh-cn/rss.xml)**（英文主源：[`/rss.xml`](https://ovh-nexus.ellery.me/rss.xml) · [`/feed.xml`](https://ovh-nexus.ellery.me/feed.xml) · 法文：[`/fr/rss.xml`](https://ovh-nexus.ellery.me/fr/rss.xml)）。
:::

本页面遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/) 规范与 [语义化版本 (SemVer)](https://semver.org/lang/zh-CN/) 记录 **OVH Nexus** 的重要变更。

## [1.4.0] - 2026-11-19 — 内存回收与目录缓存引擎重构、实时优惠情报横幅与 Debug 拆解及 MCP 优惠决策闭环

### 新增功能 (Added)

- **OVH 实时官促情报雷达、可折叠顶部横幅与双层优惠 Debug (`/servers`、`GET /api/catalog/promotions`)**：
  - 新增 `GET /api/catalog/promotions` 聚合接口，零额外目录请求输出全部生效活动卡片（`promoCode`、`context`、`autoApplied`、`capacities`、`supportedModes`、持续/最短周期、起止时间与剩余小时倒计时、最高立省金额及实时现货机型统计）。
  - 修复双通道优惠叠加解析，确保同时挂载循环月付折扣与初装费折扣的机型（如 `26game01-v4` 的 `BF26_OVH_GAME_15OFF` + `BF26_OVH_GAME_15OFF_SETUP`）完整保留 `renewPromo` 与 `setupPromo` 双明细。
  - 在服务器目录顶部新增类 OVH 官网的**可折叠官促横幅**（按活动批次记忆折叠状态、点击活动胶囊一键过滤对应机型），并内置**「优惠规则与 Debug 总览」弹窗**，直观阐明「目录自动生效 (`context="standard"`)」与「手动购物车优惠码 (`coupon`)」的边界。
  - 在单机「配置与下单弹窗」内新增**「优惠命中与计价 Debug」**面板：逐项拆解基础机型折后价、硬件升级差价（明确标示配件原价叠加/独立折扣）与初装费减免；当所选周期未命中活动（如 `upfront24` 错过 `default`/`upfront12` 活动）时提供一键切换提示；内置**防误填保护**（拦截误把自动生效活动名填入 Coupon 框的行为）与一键**「实测校验」**临时购物车实付试算功能。
- **MCP 优惠感知工具与 Agent 周期/优惠码决策闭环（共 21 个工具）**：
  - 新增免 OVH API 密钥的 L1 只读工具 **`catalog_promotions`**（L1 扩充至 **12 个工具**，总计 **21 个工具**），向 AI Agent 返回结构化活动列表、适用周期对照、`autoAppliedCodes` 与 `agentGuidance` 决策建议。
  - 升级 L2 **`order_quote`**（并透传至 **`order_create_unpaid`**）与 **`queue_add`**，全面补齐 `pricing_mode`（`default` / `upfront12` / `upfront24`）、`coupon` 与 `max_monthly_price` 参数（底层继续硬性锁死 `auto_pay=false` 与 `quantity=1` 安全红线）。

### 性能优化与重构 (Changed & Optimized)

- **磁盘缓存压降 99% 与容器内存占用深度重构**：
  - 彻底移除只写不读的 `Service.rawCatalogs` 与无调用方的 `Service.vpsCache` / `vps_*.json` 堆驻留泄漏，释放数百 MB 长期被钉死的原始 `map[string]any` 对象。
  - 废弃目录路径上的 `map[string]any` 全量反序列化，改用精简强类型结构体与串行 `json.NewDecoder` 流式解码，并将公开目录解压上限从 `32 MiB` 放宽至 `128 MiB`、HTTP 超时延长至 `60s`。
  - 在 120 秒高频 `SKUWatcher` 刷新中引入 **Gzip 流式 SHA-256 摘要短路**与局部超时防覆盖保护：上游目录未变更时实现 **0 次 JSON 解析、0 次字典重建、0 次磁盘写入**，同时完整保留 120 秒上新监测灵敏度。
  - 引入 `PlanAliases` 消除字典 5 倍结构体值拷贝，入库前剔除占比 85%+ 的软件/系统许可证家族（`application-license` / `distribution-license`），并在 `GET /api/catalog/dictionary` 输出轻量浏览器投影，将接口体积从 **56 MB 压降至 ~0.5 MB**（彻底恢复浏览器 `localStorage` 本地缓存，升级至 `ovh_nexus_catalog_dict_v4`）。
  - 磁盘缓存升级为流式原子写入的 `.json.gz`（`catalog_dict_v4_*.json.gz` 与 `servers_v4_*.json.gz`），`/data/cache/` 总占用从 **~95 MB 压降至 < 1 MB**，启动时自动清理历史 `v1`/`v2`/`v3`/`vps_*.json` 遗留文件。
  - 在 `Dockerfile` 与 `main.go` 双层默认注入 `GOMEMLIMIT=256MiB` 与 `GOGC=50`（支持 `.env` 环境变量覆盖），并在冷重建后以节流异步方式调用 `debug.FreeOSMemory()` 归还物理内存页。

### 安全加固与稳定性修复 (Security & Reliability Hardening)

- **目录并发安全、原子落盘与促销规则校准**：
  - 修复 SHA-256 短路路径原地修改共享 `*CatalogDictionary.UpdatedAt` 的数据竞争，改为不可变浅拷贝替换；将上游 SHA-256 摘要持久化至 `.json.gz`，实现重启后冷启动即时短路与 `buildServerPlans` 单边故障防覆盖。
  - 将裸金属字典独立至 `bm:` 缓存键与 `catalog_bmdict_v4_*.json.gz`（避免遮蔽全量字典中的 VPS 条目与别名），`writeGzipJSONFile` 改用唯一 `os.CreateTemp` 临时文件防止并发截断。
  - 公开目录流式读取显式拦截超限截断（`ErrResponseTooLarge`）与 Gzip CRC32 校验，补充子站白名单校验与确定性别名回退；过滤已过期/未开始/配额耗尽的促销活动，拆分 `voucherPromos` 不计入自动到手价，修复承诺期专属活动 `monthlySavings` 为 0 问题并按月均折算活动立省金额。
- **采购与 MCP 报价重验、并发飞行锁与自动活动码兜底**：
  - MCP `order_quote` → `order_create_unpaid` 补齐严格核价重验：结算前同时校验实时购物车不含税月均价与初装费，涨价超过 `€0.01` 即中止并返回 `PRICE_CHANGED_SINCE_QUOTE`。
  - `queue_add` 与队列任务新增 `max_setup_fee`（SQLite 自动列迁移），严格拒绝显式 `<= 0` 限价与非法 `pricing_mode`；区分绑券接口的 `429`/`5xx` 瞬态错误（保持退避重试）与 `4xx` 永久拒券（`INVALID_COUPON`）。
  - 新增按 `accountID + planCode + 配置指纹(options+mode+coupon)` 细粒度飞行锁并支持 `any` 机房通配防重；前后端（QuickOrder / 队列 / 快速监控 / MCP）统一自动剥离误填的目录自动活动码并记录 `WARNING` / Toast 提示。
- **会话即时吊销、SSE 防泄漏、容器与 HTTP 边界加固**：
  - 新增持久化 `session_epoch` 与 `API_SECRET_KEY` HMAC 指纹（`keyFp`）：登出、2FA 启停、恢复码重置及更换主密钥重启均立即吊销其他设备会话。
  - 日志 SSE 流（`/api/logs/stream`）增加每帧 `10s` 写超时、并发订阅上限（`32`，超限返回 `503`）、停机统一关闭以及 URL 编码敏感词脱敏。
  - 修复通配 `CORS_ORIGINS=*` 反射源与凭据头问题，为 Cookie 非幂等请求补齐 `Origin`/`Sec-Fetch-Site` CSRF 校验（放行 MCP Bearer 与 API Key），`COOKIE_SECURE=true` 时下发 HSTS；移除带副作用的 `GET /api/servers/{planCode}/price`，为手动刷新与清缓存增加 30 秒节流，`/api/cache/info` 隐去宿主机绝对路径；容器入口改用不追踪软链的 `chown -RP`，SQLite PRAGMA 编码进 DSN，Dockerfile 升至 `golang:1.27.4-alpine`；前端补齐实测询价 `AbortController` 竞态隔离、浏览器内存字典 12 小时过期、横幅基于 `endDate` 动态倒计时以及历史订单外链 `https://` 白名单。

---

## [1.3.4] - 2026-11-18 — 实时库存矩阵防挤压排版与假促销纠偏、SKU 基线自愈迁移及全站 SEO / `/llms.txt` AI 检索增强

### 新增功能 (Added)

- **三语变更日志 RSS 2.0 订阅源 (`/rss.xml`、`/feed.xml`、`/zh-cn/rss.xml`、`/fr/rss.xml`)**：
  - 在文档站构建期自动解析英、中、法三语 Changelog 并生成标准 RSS 2.0 订阅源（内含完整 HTML 变更明细与版本锚点直达链接），同时在全站 HTML `<head>` 注入 `application/rss+xml` 自动发现标签。
- **面向 AI 搜索引擎与大模型的 `/llms.txt` 与 `/llms-full.txt` 标准支持**：
  - 在文档站构建期（`astro:build:done`）自动从英文主文档生成根目录 `/llms.txt`（遵循 [llmstxt.org](https://llmstxt.org) 规范的全站结构化导航摘要与多语言入口）和 `/llms-full.txt`（剥离 MDX 组件标签后的单文件全量纯文本 Markdown 文档），配置 `text/plain; charset=utf-8` 响应头，并在 `robots.txt` 中显式放行 `GPTBot`、`OAI-SearchBot`、`ChatGPT-User`、`ClaudeBot`、`Claude-Web`、`PerplexityBot`、`Google-Extended` 与 `Applebot-Extended`。
- **IndexNow 自动主动推送与预览环境防收录隔离**：
  - 新增静态 IndexNow 密钥验证文件、`website/scripts/indexnow.mjs` 推送脚本及 `.github/workflows/docs-indexnow.yml` 工作流，在 `main` 分支文档更新上线后自动向 `https://api.indexnow.org/indexnow` 批量提交最新 URL。
  - 新增 Cloudflare Worker/Pages 边缘入口（`website/src/worker.ts` 与 `website/functions/_middleware.ts`）、构建期预览分支检测及历史归档路径隔离规则：当访问域名不是 `ovh-nexus.ellery.me`（如 `*.pages.dev`、`*.workers.dev`）时自动注入 `X-Robots-Tag: noindex, nofollow`，防止预览域名或旧版本页面分流正式站搜索排名权重。

### 问题修复 (Fixed)

- **实时库存矩阵 (`/availability`) 双行防挤压布局与浅色模式对比度修复**：
  - 重构矩阵行顶部布局：左侧型号、状态/促销/代数/新上架徽标（统一强制 `whitespace-nowrap shrink-0 inline-flex` 严禁竖排折字）与 CPU 保持单行舒展展示，右侧配置下拉框（受控宽度 `sm:w-[260px] lg:w-[300px]`）、安装费、月付价格与快速监控按钮紧凑对齐。
  - 机房单元格升级为自适应最小宽度（`min-w-[116px]`，机房国旗与代码固定 `shrink-0 whitespace-nowrap`），彻底解决显示 `1H-Low · 6配` 时将 `SBG` 等机房代码截断为单个字母的问题。
  - 修复浅色模式（Light Mode）下 `/availability` 与 `/servers` 筛选按钮及徽标文字对比度过低的问题（升级为 `text-rose-600`、`text-emerald-700`、`text-amber-700`、`text-purple-700`、`text-cyan-700`）。
- **多配置机型起始价与真实促销 (`hasPromo`) 判定纠偏**：
  - 修复 `MergedPlanRow` 在汇总多配置机型时将最低配起售价与高配原价跨配置混比、以及将原本无安装费的常规机型（`originalSetupFee === 0`）误判为免安装费促销的缺陷；现在行级起始月价与原价划线严格取自同一套最便宜配置，且仅当目录存在真实折扣或原安装费大于 0 被免除时才标记为 `🔥 促销优惠`。
- **历史存量 SKU 基线 (`baseline: true`) 保护与自愈迁移**：
  - 修复 `server/internal/monitor/sku.go` 中 `normalizeLoadedSKUs` 覆盖已有 `Baseline: true` 标记的缺陷，并新增启动期自愈迁移：结合 `ChangesJSON` 变更历史自动将从未作为 `"added"` / `"replaced"` 出现过的历史存量机型恢复为 `Baseline: true, ListedAt: ""`，消除全部非新增机型被误标为「新上架」的问题。

---

## [1.3.3] - 2026-11-17 — 密码管理器兼容登录 (`ADMIN_USERNAME`)、安装费与黑五促销展示、多周期合约下单与优惠码强校验

### 新增功能 (Added)

- **密码管理器兼容的登录用户名机制 (`ADMIN_USERNAME`)**：
  - 新增 `ADMIN_USERNAME` 环境变量（兼容别名 `NEXUS_USERNAME`，默认值 `Nexus`，严格区分大小写并与 `API_SECRET_KEY` 一同执行恒定时间校验），并将登录表单升级为包含标准 `username` + `current-password` 字段的 `<form autoComplete="on">` 结构，完美支持浏览器与密码管理器（1Password、Bitwarden、Apple 密码）自动保存与填充。
- **服务器配置选择与下单独立弹窗 (`/servers`)**：
  - 在服务器目录中新增独立的**服务器配置与下单弹窗**，集中展示合并后的标配（带 `标配` 徽章并默认选中）与可选配置、不含税预估月价、机房实时库存筛选（「显示全部库存（含无货）」勾选框）及折叠的「高级选项（带宽 / vRack）」，并在弹窗底栏直接联动「快速监控此配置」「加入抢购队列」与「立即下单」。
- **下单前安装费 / 初装费 (`installation`) 完整展示**：
  - 自动解析所有机型与硬件配置的一次性初装费（`capacities: ["installation"]`）及免安装费促销，在服务器目录（`/servers`）、实时库存矩阵（`/availability`）、配置与下单弹窗以及快速监控弹窗（`QuickMonitorDialog`）中清晰展示折后安装费 `setupFee` 与原安装费划线价 `originalSetupFee`。
- **目录促销优惠 (`promotions`) 自动识别与「🔥 促销优惠」专属筛选**：
  - 自动解析 OVHcloud 公开目录中的活动促销规则（如黑五 `20% OFF` 周期折扣与 `FREE SETUP` 免安装费），在卡片与矩阵行中直接渲染促销徽标（`🔥 -20%`、`🔥 免安装费`）、折后价 + 原价划线（`originalMonthlyPrice`），并在 `/servers` 与 `/availability` 顶部新增带实时计数角标的 **「🔥 促销优惠 (Promo)」** 筛选标签。
- **多周期合约付款模式 (`pricingMode` & `duration`)**：
  - 在配置与下单弹窗、快速监控自动下单目标（`targets[]`）与采购抢购队列（`/queue`）中全面支持月付无合约（`default` / `P1M`）、12 个月一次性年付（`upfront12` / `P12M`）、24 个月一次性两年付（`upfront24` / `P24M`）及合约月付（`degressivity12` / `degressivity24`）。
  - 切换付款周期时实时联动更新周期总价、折合月均不含税价（`monthlyEquivalentPrice`）及安装费减免（如切换至 `upfront12`/`upfront24` 自动免除安装费 `€0.00`），并统一按**折后折合月均不含税价（`周期总价 / 月数`）**与最高月价上限 `maxMonthlyPrice` 比对。
- **自定义优惠码 (`coupon`) 支持与失效立停保护 (`400 INVALID_COUPON`)**：
  - 在配置下单弹窗、快速监控目标与采购队列中支持填入可选自定义优惠码（`coupon`），下单时自动调用 `POST /order/cart/{cartId}/coupons`；若 OVHcloud 拒绝该优惠码，立即中止结算并返回 `400 INVALID_COUPON`（同步清理临时购物车、记录 `WARNING` 日志并发送 `order.failed` 告警），严防优惠码失效或输错时按原价误下单。

### 变更与优化 (Changed)

- **服务器目录 3 列固定高度模块化卡片网格 (`/servers`)**：
  - 将服务器目录重构为 3 列固定高度卡片网格（`grid-cols-1 md:grid-cols-2 xl:grid-cols-3`），采用 2×2 模块化硬件规格宫格（**CPU**、**内存**、**存储**、**带宽**）、独立固定行高的机房库存状态行与固定底部操作栏，确保全列表卡片高度与视觉基线严丝合缝。
- **全新蓝色半透明流体玻璃 “N” 品牌视觉统一**：
  - 将全站品牌 Logo、站点图标（`favicon.svg`、`favicon.ico`、`apple-touch-icon.png`）与 OpenGraph 社交预览横幅统一升级为全新的蓝色半透明流体玻璃（Liquid Glass）“N” 视觉设计。

---

## [1.3.2] - 2026-11-17 — 已公布未开售机型追踪、按配置精确监控、价格保护上限与紧凑视图重塑

### 新增功能 (Added)

- **已公布未开售状态 (`listingState: "announced"`) 与开售 (`saleStarted`) 自动提醒**：
  - 识别已写入 OVHcloud 公开价格目录（`eco` / `baremetalServers`）但尚未在实时机房库存接口开放的机型（如 `KS-LE`、`RISE-LE`、`SYS-LE` 限量版系列），在实时库存矩阵（`/availability`）与服务器目录（`/servers`）中提供**已公布未开售（Announced）**徽标与筛选器。
  - 当 `announced` 机型首次出现在实时库存接口时，自动生成带 `saleStarted: true` 的变动记录并推送**正式开售（Sale Started）**通知。
- **产品线同代换号 (`replaced`) 识别与分组聚合通知**：
  - 自动识别同一产品线（同型号名称 + CPU）在单轮扫描中的 `planCode` 换代（如 `24sys022 -> 24sys02-v3`），合并为单条 `replaced`（含 `replacedFrom`）时间线事件，不再同时误报新增与下架；并将 `sku.changed` 通知按产品线归类为**新增、开售、换代、下架**四个分组。
  - MCP 网关 `catalog_availability` 与 `catalog_sku_changes` 工具同步补齐 `listingState`（`listed` / `announced` / `presale`）、`saleStarted` 与 `replacedFrom` 字段。
- **机型配置明细与预估月价接口 (`GET /api/catalog/plans/{planCode}/configs`)**：
  - 返回指定 `planCode` 合并后的标配与全部可选硬件配置列表，包含 `configKey`（`内存+硬盘`指纹）、三语可读规格、附加项 `options`、标配标识 `isDefault`、不含税预估月价 `estimatedMonthlyPrice`（基础月价 + 内存/硬盘附加月费）、币种及各机房实时库存状态。
- **多目标配置级库存监控 (`targets[]`) 与购物车实时月价上限保护 (`maxMonthlyPrice`)**：
  - 库存监控订阅升级为多目标结构（`StockSubscription.targets[]`），每个目标可独立设置具体硬件配置（`configKey` + `options`，仅通知模式可选「任意配置」）、机房列表、到货/断货通知开关、自动下单开关、采购数量 `quantity` 以及不含税**最高月价上限（`maxMonthlyPrice`）**。
  - 自动下单结算前会读取购物车实时报价中的**不含税月维护费**（安装费单独展示、不计入月价上限）并与 `maxMonthlyPrice` 比对；若超限则拒绝下单（`PRICE_ABOVE_LIMIT`），记录 `WARNING` 日志并发送 `order.failed` 告警通知。

### 变更与优化 (Changed)

- **紧凑单行实时库存矩阵 (`/availability`)**：
  - 按 `planCode` 合并为**一机一行**（`-sgp`、`-syd`、`-mum` 等区域独立 `planCode` 仍各自成行），行内提供配置下拉框，支持在「全部配置」（展示各机房最佳库存状态与有货配置数）与任意具体配置（展示该配置不含税月价、有货机房数与各机房库存）之间即时切换。
- **紧凑可折叠服务器目录卡片 (`/servers`)**：
  - 桌面端改为每行 3～4 张的默认折叠紧凑卡片网格（仅展示型号、CPU、单行标配摘要、起始月价、有货机房紧凑国旗徽标与快捷操作按钮）。
  - 卡片展开后将原本分离的「默认配置」与「可选配置」合并为**统一可选配置列表**（标配带徽标且默认选中，默认仅展示有货配置与机房，支持勾选「显示全部库存（含无货）」），并将带宽、vRack 等放入默认收起的「高级选项」；点击「加入采购队列」或「快速监控」时自动带入已选配置与机房。
- **SKU 变动监控提速与 20 秒快速复核确认**：
  - 默认扫描周期由 `600` 秒缩短为 `120` 秒（已保存的旧默认值 `600` 自动平滑迁移为 `120`），独服 SKU 扫描通过 `GetBaremetalDictionary` 与 VPS 目录解耦提速；当检测到某 SKU 缺失时，约 `20` 秒内自动发起单独复核，确认缺失后立即推送下架通知，无需等待下一完整周期。
- **旧版自动下单订阅安全迁移 (`needsConfigSelection`)**：
  - 升级后旧版库存订阅会自动转换为「任意配置 + 原机房」通知目标；若旧订阅曾开启 `autoOrder` 但未指定具体配置，系统会自动暂停其自动下单并标记 `needsConfigSelection: true`，在监控页展示醒目横幅引导用户选择具体配置与最高月价后重新开启。

### 问题修复 (Fixed)

- **目录字典别名污染清理与预售状态精准判定**：
  - 在 `CatalogDictionary` 中新增仅记录 `eco` 与 `baremetalServers` 真实 `planCode` 的 `RealPlans`，将 `baseCode` 与 `product` 别名严格限制为元数据查找键；静默清理历史残留的别名 SKU（如 `24sys02-1g`、`27risele01`），不触发假下架通知，并修复 `presale` / `listed` / `announced` 状态判定与非基线记录的 `listedAt` 时间回填。
- **移除自动下单未指定配置时的盲选兜底并落实采购数量**：
  - 移除 `stock-monitor` 自动采购在未传入 `options` 时自动回退选择第一个有货配置的高风险兜底逻辑（强制要求 `CONFIG_REQUIRED_FOR_AUTO_ORDER`），并在创建购物车时严格应用目标配置的 `quantity` 采购数量。

---

## [1.3.1] - 2026-11-17 — 预售机型识别、一键快速监控、三语机房地理信息字典与 MCP 网关增强

### 新增功能 (Added)

- **预售 / 待上架服务器自动识别与筛选**：
  - 自动识别已出现在 OVHcloud 实时机房库存接口中但尚未写入公开价格目录的预售/未发布 `planCode`，在实时库存矩阵（`/availability`）与服务器目录（`/servers`）中统一展示**预售（Presale）**徽标、硬件代数标签（`2024`–`2027`）及**仅看预售（Presale Only）**筛选开关。
- **一键快速监控弹窗 (`QuickMonitorDialog`)**：
  - 支持在 `/availability` 或 `/servers` 任意机型行直接点击配置机房库存告警、硬件子配置过滤条件与自动下单规则。
- **全局三语机房地理位置字典**：
  - 在所有 WebUI 页面与通知模板中引入三语本地化城市、国家与国旗 Emoji 标签（`en` / `fr` / `zh-CN`，例如 `BHS (Beauharnois, Canada 🇨🇦)` / `BHS（博阿努瓦，加拿大 🇨🇦）`，以及紧凑位置带悬停全名提示的 `🇨🇦 BHS` 徽标），并新增多伦多（`TOR` / `ca-east-tor`）机房支持。
- **库存监控与自动采购队列来源联动**：
  - 由库存监控触发生成的采购队列任务自动记录 `source: "stock-monitor"`，支持在 `/monitor` 与 `/queue` 之间双向跳转定位，并收敛为后端单点触发（移除浏览器前端重复的 `quick-order` 调用）。
- **MCP 网关体验与工具增强**：
  - `/mcp` 控制台配置复制新增**仅服务器条目（Server Entry Only）**与**完整配置文件（Full Config File）**双模式切换及 JSON 合并逗号提示；`catalog_availability` 新增 `offset` 分页与 `presale_only` 预售筛选参数；`catalog_sku_changes` 数据源与 SKU Watcher 变动时间线完全对齐。

### 问题修复 (Fixed)

- **首次部署存量 SKU 基线静默与两阶段上架追踪**：
  - 首次初始化扫描时的存量机型标记为 `baseline: true`（不再误标为新上架），并分别记录 `presaleSince`（首次预售出现时间）与 `listedAt`（转为公开目录正式上架时间），仅基线之后的真实新上架及 `presale -> listed` 转正机型显示为**新上架（New）**。
- **多伦多 (`ca-east-tor`) 机房购物车结算区域推断修复**：
  - 修复 `InferCartRegion`，将 `tor` / `ca-east-tor*` 机房正确映射至 `"canada"` 结算区域。
- **安全依赖升级**：
  - 将 `server/go.mod` 中的 `golang.org/x/oauth2` 升级至 `v0.38.0`，并清理未使用的传递依赖 `google.golang.org/protobuf`（修复 `CVE-2025-22868`、`CVE-2024-24786`）。

---

## [1.3.0] - Go 1.27 单静态二进制架构、SQLite WAL 持久化与 SSE 实时结构化日志

:::caution[v1.3.0 破坏性变更提示]
`v1.3.0` 引入了 Go 单静态二进制运行时（监听 `:8080`）、内嵌 SQLite WAL 数据库（`/data/nexus.db`，**不支持**从 `<= v1.2.3` 的 JSON 状态文件自动迁移）、纯出站通知中心与全标准化英文 `slog` 日志架构。从 `<= v1.2.3` 升级前请务必阅读下方的**破坏性变更**说明。
:::

### 破坏性变更 (Breaking Changes)

- **容器监听端口与单卷持久化布局 (`docker-compose.yml`)**：
  - 容器内部 HTTP 监听端口由 `:80` 变更为 `:8080`（`ports: ["20000:8080"]`），原有指向容器 `:80` 端口的映射或反向代理目标需同步调整为 `:8080`。
  - 所有持久化目录统一整合为单个 `./data:/data` 挂载卷，移除独立的 `logs`/`cache` 挂载以及旧版环境变量 `BIND_HOST`、`DEBUG` 和 `ENABLE_API_KEY_AUTH`。
- **内嵌 SQLite WAL 存储 (`/data/nexus.db` — 需使用全新 `./data` 目录)**：
  - 使用单文件 `/data/nexus.db` 取代分散的 JSON 状态文件；**不提供从 `<= v1.2.3` JSON 文件的自动迁移**。从旧版本升级时须初始化全新干净的 `./data` 目录，并重新绑定 2FA（TOTP）与 OVHcloud API 账户凭据。
- **纯出站通知中心**：
  - 移除入站 Telegram Webhook 回调路由（`/api/telegram/*`），聚焦纯出站消息推送（Telegram Bot API 与 SMTP 邮件），支持通过可选 `panelUrl` 携带控制台直达链接。
- **全标准化英文结构化日志**：
  - 以标准化英文 `slog` 结构化日志（UTC RFC3339 时间戳）取代本地化非结构化文本日志，并提供 SSE 实时推流端点（`/api/logs/stream`）。

### 变更与优化 (Changed)

- **Go 1.27 单静态二进制架构 (`server/`)**：
  - 全面采用 **Go 1.27 单静态二进制**（`CGO_ENABLED=0`），基于 `net/http`、`chi/v5` 与 `github.com/ovh/go-ovh` 构建，并通过 `//go:embed` 将 React SPA 直接内嵌至可执行文件中。
  - 原生在 `:8080` 端口提供不可变资源缓存、CSP 安全响应头、Gzip 压缩以及 Slowloris / 请求洪水防御（`ReadHeaderTimeout: 5s`、`ReadTimeout: 15s`、`WriteTimeout: 120s`、`IdleTimeout: 60s`、`2 MB` 请求体上限）。
- **极致精简非 Root 容器（约 `20 MB`，`su-exec` 启动降权、`PUID`/`PGID`）**：
  - 更新三阶段 `Dockerfile`（`node:24-alpine` -> `golang:1.27-alpine` -> `alpine:3.24`），内置 `su-exec` 与 `/usr/local/bin/docker-entrypoint.sh`，启动时自动初始化 `/data`（`0700`）并就地降权至 `PUID:PGID`（默认 `65532:65532`，消除首次部署宿主机 `./data` 权限报错，同时无缝兼容 Kubernetes `runAsNonRoot: true`）。
  - 精简 `docker-compose.yml`，端口映射调整为 `"20000:8080"`，仅需挂载**单个持久化卷 `./data:/data`**，支持可选 `PUID`/`PGID` 环境变量透传。
- **纯 Go 内嵌 SQLite WAL 数据库 (`/data/nexus.db`)**：
  - 将账户注册表、TOTP 2FA 状态、采购队列、订单历史、独服/VPS 库存订阅、SKU 基线与时间线、通知渠道、MCP 配置/代号/审计与系统日志统一收纳至单文件 ACID 数据库 `/data/nexus.db`（`modernc.org/sqlite`，`WAL` 模式，目录 `0700` / 文件 `0600` 权限）。
  - 延续字段级 `HKDF-SHA256` + `AES-256-GCM` 静态加密（`enc:v1:...`）保护所有 API 密钥、Telegram Token、SMTP 密码与 TOTP 种子，MCP Bearer 令牌与应急恢复码均以 `SHA-256` 哈希保存。
- **统一快捷下单路径与纯出站多语通知中心**：
  - 统一快捷下单端点为 `/api/orders/quick-order`（默认 `auto_pay=false` 且 `waive_retractation_period=false`）。
  - 精简**通知中心**为纯出站推送引擎（Telegram Bot API 与 SMTP 邮件），新增独立**通知语言选择 (`locale`：`en`、`fr`、`zh-CN`)** 与容器 `TZ` 时区支持（默认 `UTC`）。

### 新增功能 (Added)

- **全标准化英文 `log/slog` 日志中枢与 SSE 实时控制台 (`/logs`)**：
  - 统一通过 14 个规范 `source` 模块（`server`、`http`、`auth`、`totp`、`vault`、`ovh`、`catalog`、`queue`、`stock-monitor`、`vps-monitor`、`sku-watcher`、`fleet`、`notify`、`mcp`）输出标准化英文结构化日志，并自动脱敏敏感凭据。
  - 数据库内置 **10,000 条滚动环形日志缓冲区**（`system_logs`），支持 **Server-Sent Events 实时推流 (`GET /api/logs/stream`)** 与 **`.jsonl` / `.log` 一键导出**（`GET /api/logs/export`）。
  - 全面升级 `LogsPage.tsx`：三语界面自适应、模块/级别/关键词多维筛选、结构化属性标签展示与本地时区时间格式化。
- **可信代理 IP 解析 (`TRUSTED_PROXIES`) 与时区配置 (`TZ`)**：
  - 新增 `TRUSTED_PROXIES` IP/CIDR 白名单校验，仅采信来自受信任反向代理的 `X-Forwarded-For` / `X-Real-IP` 请求头；新增 `TZ` 时区配置。

---

## [1.2.3] - 响应式侧边栏布局修复、首次部署 OVH API 引导闭环、2FA 状态同步与文档站 SEO 增强

### 问题修复 (Fixed)

- **Tailwind CSS v3 响应式选择器转义异常与侧边栏消失修复**：
  - 将 `package.json` `overrides` 中的 `postcss-selector-parser` 锁定为 `^6.1.6`（修复误解析至 `7.2.0` 导致 `.lg\:block` 等响应式类名被三重转义失效、桌面端左侧导航栏被隐藏的问题）。
  - 在 `Layout.tsx` 中补齐 `< 1024px` 紧凑视口顶部导航栏与菜单按钮，彻底消除 `768px–1023px` 视口盲区。
- **首次部署 OVH API 填写入口闭环与顶部提示条优化**：
  - 在左侧主导航新增 **「OVH API 账户」(`/api-accounts`)** 入口，并在「系统与安全设置」(`/settings`) 中增加多账户管理卡片跳转。
  - 将顶部「立即配置 API」按钮直连 `/api-accounts` 填写表单，在配置页自动隐藏重复提示条并支持一键收起关闭，同时补齐首次绑定 2FA 的前置引导提示。
- **2FA (TOTP) 绑定后状态同步修复**：
  - 统一后端 `/api/auth/totp/status` 与 `/api/auth/totp/activate` 返回的 `totpEnabled` / `enabled` 字段及错误提示字段（`message`），修复绑定验证成功后前端仍显示“未启用”的问题。

### 新增功能 (Added)

- **文档门户 SEO 与搜索引擎抓取优化 (`website/`)**：
  - 新增构建期 `/sitemap.xml` 直出生成（与 `/sitemap-index.xml` 同步输出），注入 `<lastmod>` 更新日期与 `hreflang="x-default"` 多语言互链；完善 `robots.txt` 搜索引擎放行规则、`_headers` MIME 响应头与 OpenGraph 社交预览图（`og-image.jpg`）。

---

## [1.2.2] - 依赖基线升级与 Dependabot 跨大版本治理加固

### 变更与升级 (Changed)

- 升级安全与文档门户依赖基线（`website/package.json` 中 `@astrojs/starlight@^0.42.11`）。
- 完善 `.github/dependabot.yml` 中前端（`/`）与文档站（`/website`）的 `ignore` 策略，显式锁定 `react` / `react-dom`（`<19.0.0`）、`tailwindcss`（`<4.0.0`）与 `recharts`（`<3.0.0`）大版本边界，并全局屏蔽非安全类的 `semver-major` 跨大版本自动 PR。

---

## [1.2.1] - AES-256-GCM 磁盘静态加密、TOTP 2FA 分级安全门禁、欧盟 14 天撤回权保护与 Node 24 LTS 升级

### 安全加固 (Security)

- **本地磁盘敏感凭证静态加密 (`DATA_ENCRYPTION_KEY`)**：
  - 新增必填且独立的环境变量 `DATA_ENCRYPTION_KEY`（长度不少于 16 位，缺失、弱密钥或与 `API_SECRET_KEY` 相同时启动期直接阻断报错）。
  - 基于 `HKDF-SHA256` 密钥派生与 `AES-256-GCM` 认证加密算法（`enc:v1:<nonce>:<ciphertext>`），对所有敏感凭据（`appKey`、`appSecret`、`consumerKey`、Telegram Bot Token、SMTP 密码、TOTP 种子及会话密钥）全面施行落盘加密。
  - 升级 MCP 令牌存储结构，仅持久化不可逆的 `SHA-256` 哈希摘要（`tokenHash`）与脱敏预览（`maskedToken`）。
- **TOTP 双因素认证 (RFC 6238) 与分级高危操作安全门禁**：
  - 原生实现 RFC 6238 TOTP 动态口令认证，支持离线二维码绑定、30 秒窗口防重放（`lastUsedStep`）以及 8 个一次性应急恢复码（`XXXX-XXXX`）。
  - 全局强制高危门禁：**未绑定启用 2FA 时，所有高危操作路由一律返回 `403 TOTP_SETUP_REQUIRED` 拦截，且 MCP 网关 `L3`（可逆运维）被底层强制锁定关闭**。
  - 双层 Step-Up 验证体验：`strict` 级（重装系统、服务退订销毁、过户转移、新增/编辑/删除 API 账户密钥、关闭 2FA）要求每次请求均附带最新动态码；`sudo` 级（独服/VPS 重启、救援模式、IPMI 控制台、边缘防火墙/网络变更、MCP 配置变更、开启自动扣款或放弃撤回权）验证一次后授予当前会话 **5 分钟免重复输入窗口**。
- **欧盟 14 天消费者撤回权保护与支付风控 (`waiveRetractationPeriod` & `auto_pay`)**：
  - 将下单引擎与采购队列中的 `waiveRetractationPeriod` 设为**默认 `false`**（默认保留欧盟 14 天法定无理由撤回权），并在队列与服务器目录页提供任务级双开关（`auto_pay` + `waive_retractation_period`），开启任意一项均需通过 2FA Sudo 验证；MCP 工具层硬编码锁死两者为 `false`。

### 变更与升级 (Changed)

- 将容器构建基础镜像与 CI 运行时升级至 **Node 24 LTS (`node:24-alpine`)**，升级 GitHub Actions（`actions/setup-node@v7`、`docker/setup-buildx-action@v4`、`peter-evans/dockerhub-description@v5`）与前端依赖（`lucide-react@^1.72.0`、`sonner@^2.0.8`、`autoprefixer@^10.6.2`、`@radix-ui/react-alert-dialog@^1.2.1`、`@radix-ui/react-tooltip@^1.4.1`、`wrangler@^4.160.1`、`qrcode@^1.5.4`），并将 `.github/dependabot.yml` 目标分支切换为 `develop` 及开启分组更新策略。

---

## [1.2.0] - MCP AI 网关、双向隐私混淆护盾与“免付款下单”安全沙盒

### 新增功能 (Added)

- **内置 Model Context Protocol (MCP) Streamable HTTP 网关 (`/api/mcp`)**：
  - 对标官方 `ovh/ovhcloud-cli` 交互范式，内置遵循 JSON-RPC 2.0 标准的无状态 Streamable HTTP 端点；默认安全关闭，采用与 WebUI 登录密码彻底隔离的专属高熵令牌（`nxs_mcp_...`）鉴权，并内置按 IP 防爆破限流。
  - **三级可配置权限沙盒（共 20 个 MCP 工具）**：
    - `L1 只读查询`（11 个工具）：脱敏后的多账户画像、独服列表与详情、VPS 列表、边缘防火墙状态、服务到期倒计时、订单 4 步交付进度、全球机房实时库存与 SKU 上新变动。
    - `L2 免付采购`（5 个工具）：两步确认生成【未付款订单】（`order_quote` 签发 5 分钟报价单 → 用户确认后调用 `order_create_unpaid`）、采购队列管理与库存监控订阅。
    - `L3 可逆运维`（4 个工具）：独服/VPS 硬件重启、重置卡死的 IPMI 会话、边缘防火墙规则与永久防洪开关。
  - **L4 代码级永久财务与高危红线**：
    - 在后端代码层无条件锁死 `auto_pay = false` 与 `quantity = 1`（绝不触发自动扣款），内置每小时下单速率限制，并永久封禁重装系统、退订销毁服务、读取带令牌的 KVM 远程控制台链接及修改 SSH/API 密钥。
- **双向隐私混淆护盾 (`Balanced Shield`)**：
  - 自动将 OVH 账户（`acct-01`）、包含 IP 段的独服主机名（`bm-01`）、VPS（`vps-01`）、公网 IP（`ip-01`，仅保留末段尾号 `*.*.*.42`）与订单号（`ord-01`）映射为持久化代号，并彻底剥离含 `orderPassword` 的付款链接与个人敏感信息。
- **WebUI「MCP AI 网关」控制台 (`/mcp`) 与通知中心审计事件 (`mcp.action`)**：
  - 在侧边栏「账户与系统」分组新增 **MCP AI 网关** 页面，支持令牌轮换/吊销、L1–L3 权限开关、多账户可见性白名单、一键复制客户端配置（Claude Code、Claude Desktop、Cursor、Antigravity）、隐私混淆映射表查阅，以及 L2/L3 写操作实时触发 `mcp.action` 通知告警。

---

## [1.1.1] - 测试依赖安全补丁

### 安全修复 (Security)

- 修复上游测试依赖安全漏洞 `CVE-2025-71176`（`GHSA-6w46-j5rx-g56g`），实现全仓依赖 Dependabot 告警清零。

---

## [1.1.0] - 依赖链安全加固清零、三语系统架构图与文档体验升级

### 安全修复 (Security)

- **全仓依赖安全告警清零 (`0 vulnerabilities`)**：
  - 将主项目中的 `react-router-dom` 升级至 `>= 7.18.5`、`vite` 升级至 `>= 8.3.6`，并将文档站 `website/` 中的 `astro`（`>= 7.4.7`）、`@astrojs/starlight`（`>= 0.42.10`）与 `sharp`（`>= 0.35.5`）升级至最新安全版本。
  - 在主目录与 `website/` 的 `package.json` 中通过 `overrides` 强制锁定构建期子依赖安全版本（`esbuild >= 0.28.1`、`glob >= 14.0.2`、`minimatch >= 11.0.1`、`postcss-selector-parser >= 6.1.5`、`zod >= 4.6.6`），彻底消除 GitHub Dependabot 报出的全部 31 条安全告警。

### 新增功能 (Added)

- **三语高清系统架构图**：
  - 为英、法、中三语《项目简介》页面新增本地化高清系统架构图。
- **文档站首页版权底栏与外链新窗口跳转**：
  - 在文档站首页底部新增年份自动更新的版权与协议底栏（`© <年份> OVH-Nexus · AGPL-3.0`），并为所有外站链接自动启用新窗口打开（`target="_blank"`）。

### 变更与优化 (Changed)

- **Docker Node 22 LTS 构建升级与 README 开箱即用配置**：
  - 将 `Dockerfile` 第一阶段 `frontend-builder` 基础镜像由 `node:18-alpine` 升级至长期支持版 `node:22-alpine` (LTS)。
  - 完善 `README.md`、`docs/README.fr.md` 与 `docs/README.zh-CN.md` 的快速开始章节，提供完整的独立 `docker-compose.yml` 与 `.env` 配置示例，以及外站链接新窗口跳转属性。
  - 修复文档站首页卡片图标键名（`telegram` 与 `padlock`）。

---

## [1.0.1] - 官方文档站上线与 CI/CD 构建优化

### 新增功能 (Added)

- **官方三语文档站点 (`website/`)**：
  - 基于 **Astro Starlight** 与 **Pagefind** 离线全文检索引擎构建全量三语（`en`、`fr`、`zh-CN`）官方文档站，覆盖 5 大章节共 69 个页面（`https://ovh-nexus.ellery.me`）。
  - 配置基于 **Cloudflare Workers Static Assets** 的纯静态资产部署架构（`wrangler.jsonc`，关闭 `workers_dev` 与 `preview_urls`），并配置严格的 CSP 与缓存响应头（`_headers`）。

### 变更与优化 (Changed)

- **仓库 README 精简与 CI 触发隔离**：
  - 精简 `README.md`、`docs/README.fr.md` 与 `docs/README.zh-CN.md`，提供 30 秒快速开始与文档站直达导航。
  - 在 `.github/workflows/dockerhub.yml` 中配置 `paths-ignore` 忽略规则，并在 `.dockerignore` 中排除 `website/`，避免纯文档变更触发不必要的 Docker 镜像重编译。

---

## [1.0.0] - 首个公开正式版发布

### 新增功能 (Added)

- **独立服务器、VPS、订单交付流水线、续费矩阵、实时库存与价格、公开货架 SKU 监控及多通道通知中心**：
  - 首个公开正式版发布，集成多账户管理、MRTG 流量、IPMI/KVM、自动化系统重装、IP 边缘防火墙、4 步硬件交付追踪、到期倒计时、FQN 配置级定价、SKU 防误报监控、Telegram/SMTP 通知与中英法三语自适应明暗主题。
