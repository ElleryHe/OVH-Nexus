---
title: 参与贡献指南
description: OVH Nexus 本地开发环境搭建、测试规范与 Pull Request 提交指南。
---

感谢您对 **OVH Nexus** 开源项目的关注与支持！我们非常欢迎代码改进、Bug 修复、多语言翻译校对以及文档完善方面的贡献。

## 安全与凭据保护红线

:::danger[严禁提交真实凭据或隐私信息]
请勿在代码、单元测试、Issue 或 Pull Request 中包含任何真实的 OVHcloud API 密钥（`appKey`、`appSecret`、`consumerKey`）、`API_SECRET_KEY`、Telegram Bot Token、SMTP 密码或未脱敏的真实服务器名称与公网 IP。

如果您发现了安全漏洞，**请勿提交公开 Issue**，请通过 [GitHub 私有安全通告 (Security Advisories)](https://github.com/ElleryHe/OVH-Nexus/security/advisories/new) 私密报告。
:::

## 本地开发环境搭建

### 前置依赖

- **Docker & Docker Compose**（用于完整容器化构建验证）
- **Node.js 18+**（构建 `website/` 文档站建议使用 Node.js 22）
- **Python 3.11+**

### 本地启动前后端与文档站

1. **前端控制台（Vite 热更新）**：
   ```bash
   npm ci
   npm run dev
   ```
2. **后端服务（Flask）**：
   ```bash
   cd backend
   pip install -r requirements.txt -r requirements-dev.txt
   API_SECRET_KEY=dev-local-random-secret-key PORT=19998 python app.py
   ```
3. **官方文档站（`website/`）**：
   ```bash
   cd website
   npm ci
   npm run dev
   ```

## 测试与构建校验

在提交 Pull Request 前，请确保通过以下全部检查：

```bash
# 1. 运行后端安全与功能单元测试套件
pytest backend/tests/test_security.py

# 2. 验证前端 TypeScript 类型检查与生产打包
npm run build

# 3. 验证文档站静态构建与死链检查（如修改了文档）
cd website && npm run build
```

## Pull Request 规范

- **遵循 Conventional Commits 提交信息规范**：如 `feat(scope): ...`、`fix(scope): ...`、`docs: ...`、`chore: ...`。
- **保持三语同步**：新增界面文案或文档章节时，请同步维护 `en`、`fr` 与 `zh-CN` 三个语种。
- **明暗主题兼容**：修改 UI 组件时，请同时在**浅色模式（纯粹白）**与**深色模式（中性碳黑）**下验证视觉对比度。
