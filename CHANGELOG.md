# 更新记录

本项目采用 [Keep a Changelog](https://keepachangelog.com/) 风格记录面向使用者的变化。版本号将在首次公开发布时确定。

## Unreleased

### Added

- 开源工程文档、贡献指南、安全策略、CI 与 Dependabot 配置
- 匿名用户和项目导入示例
- 不含人物与内部资料的默认登录背景
- `AGPL-3.0-only` 许可证、`3_stones` 版权声明与公共资产授权说明

### Changed

- Go 构建工具链下限提升到 1.26.6
- 公开部署样例和用户手册移除生产域名
- 默认背景种子仅在空图库中写入，兼容现有私有图库

### Security

- 扩充忽略规则，阻止数据库、备份、密钥、名单、照片和构建产物进入仓库
- 使用 `govulncheck` 检查依赖和标准库
