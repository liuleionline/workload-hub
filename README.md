# 载衡 · 人力负荷管理平台

![载衡标识](web/static/logo-mark.svg)

载衡是面向设计部门的轻量级人力与工作负荷管理系统。它用周工时、项目负责人预估、请假与工作日历生成个人、项目和部门看板，并提供分层权限、周期统计、Excel 导出、审计和备份恢复。

项目采用 Go 单体应用和 SQLite，页面、静态资源与数据库迁移都编译进一个可执行文件，适合小团队本地运行或在自有环境中使用。

## 主要功能

- 设计师、专业负责人、部门领导和系统管理员的分层权限
- 每周实际工时、请假、临时工作和工地驻场填报
- 专业负责人按项目预估下周人员投入
- 项目阶段、子项、参与人员、完成与归档管理
- 周、月、季、年员工与项目看板
- 同员工、同周、同项目口径的偏差系数和修正负荷
- 个人与部门 Excel 导出、浏览器打印 PDF
- 登录背景图库、操作审计、数据库备份与恢复
- 测试用户数据与正式部门统计隔离

## 技术结构

```text
浏览器 ──HTTP(S)── Go 应用 ── SQLite
                         │
                         ├─ HTML 模板与静态资源（go:embed）
                         └─ 备份、恢复与导出
```

更详细的边界、权限和数据流见 [架构说明](docs/ARCHITECTURE.md)。

## 环境要求

- Go 1.26.6 或更高的安全补丁版本
- Windows、macOS 或 Linux 均可用于本地开发
- 运行方式和网络环境由使用者自行选择
- 无需 MySQL、Redis、Node.js 或 Docker

## 快速开始

1. 复制环境变量示例：

   ```powershell
   Copy-Item .env.example .env
   ```

   Linux/macOS 可执行 `cp .env.example .env`。

2. 创建首个系统管理员：

   ```powershell
   go run . init-admin --email admin@example.com --name 系统管理员
   ```

3. 启动服务：

   ```powershell
   go run . serve
   ```

4. 打开 `http://127.0.0.1:8080`，首次登录后修改临时密码。

应用会自动创建数据目录、SQLite 数据库、基础权限、默认设置和两张不含人物的抽象登录背景。

## 导入示例数据

仓库提供匿名 CSV 示例：

```powershell
go run . import-users --file examples/initial-users.csv
go run . import-projects --file examples/initial-projects.csv
```

导入用户前数据库中不能已有用户。项目 CSV 中引用的创建人和专业负责人必须先存在。实际员工名单、项目清单和身份证信息不得提交到公开仓库。

## 配置

|变量|默认值|说明|
|-|-|-|
|`APP_ADDR`|`127.0.0.1:8080`|监听地址|
|`APP_DB`|`./data/workload.db`|SQLite 数据库路径|
|`APP_BACKUP_DIR`|`./backup`|备份目录|
|`APP_COOKIE_SECURE`|`false`|HTTPS 生产环境必须设为 `true`|
|`APP_BASE_URL`|`http://127.0.0.1:8080`|外部访问地址|
|`APP_TIMEZONE`|`Asia/Shanghai`|业务时区|

仓库不包含云主机、账号、域名、反向代理或进程守护配置；运行环境由使用者自行准备。

## 开发与验证

```powershell
go mod verify
go vet ./...
go test ./... -count=1
go build -buildvcs=false -trimpath .
```

GitHub Actions 会在推送和拉取请求中执行依赖校验、静态检查、竞态测试、构建与 `govulncheck`。

## 数据与隐私

数据库、备份、导入表格、访问凭据、内部交接资料、真实员工照片和构建产物都由 `.gitignore` 排除。准备提交前仍应运行：

```powershell
git status --short
git ls-files
```

逐项确认没有姓名、邮箱、手机、项目资料、工时、密码哈希、访问令牌或运行环境地址。

## 已知边界

- 当前业务模型以单部门为主，多部门属于后续扩展方向。
- 密码最低长度保留为 6 位以兼容现有部署；公开部署建议使用更强临时密码，并在首次登录后立即更换。
- 内置工作日历目前包含 2026 年中国法定节假日，后续年度需由管理员维护。
- 浏览器保存 PDF；项目当前没有服务端 Word 导出。
- 自动备份调度与异地存储不属于本仓库范围；应用仅提供通用的备份、下载和恢复能力。

## 参与贡献与安全问题

贡献前请阅读 [CONTRIBUTING.md](CONTRIBUTING.md)。安全问题不要公开创建普通 Issue，请按 [SECURITY.md](SECURITY.md) 使用私密渠道报告。

## 许可证

Copyright (C) 2026 `3_stones`。

除文件中另有说明外，项目源代码、文档、Logo、图标和公共视觉素材均采用 [GNU Affero General Public License v3.0 only](LICENSE)（`AGPL-3.0-only`）。通过网络向用户提供修改后版本时，应按许可证要求向这些用户提供对应源代码。

版权与资产授权范围见 [NOTICE](NOTICE)，第三方组件许可证见 [THIRD_PARTY_LICENSES.md](THIRD_PARTY_LICENSES.md)。
