# 512MB Debian VPS部署说明

## 推荐运行结构

- Caddy：HTTPS和反向代理
- 载衡单体程序：页面、权限、统计和定时逻辑
- SQLite：与程序位于同一台VPS的本地磁盘
- systemd：进程守护

不需要MySQL、PostgreSQL、Redis、Docker或服务器端浏览器。

## 目录

- 程序：`/opt/workload-hub/workload-hub`
- 数据库：`/var/lib/workload-hub/workload.db`
- 环境配置：`/etc/workload-hub.env`
- 备份：`/var/backups/workload-hub/`

## 上线顺序

1. 创建不可登录的 `workload-hub` 系统用户。
2. 安装程序到 `/opt/workload-hub/`。
3. 创建数据目录并仅授予该用户读写权限。
4. 复制并修改 `deploy/workload-hub.env.example`。
5. 使用匿名样例制作你自己的私有导入文件：先运行 `import-users --file examples/initial-users.csv`，再运行 `import-projects --file examples/initial-projects.csv`。真实名单与项目表不要提交到仓库；上传到服务器导入后，应立即删除含身份证后六位等初始密码信息的明文CSV。
6. 安装并启用systemd服务。
7. 安装Caddy，设置域名DNS后启用`deploy/Caddyfile`。
8. 防火墙只开放SSH、80和443；SSH使用密钥登录。
9. 设置 `APP_BACKUP_DIR=/var/backups/workload-hub`，安装每周六18:00备份定时任务；VPS只保留最近3份，并由系统管理员通过网页下载到VPS之外存档。
10. 如启用阿里云盘异地备份，按 `docs/阿里云盘备份接入.md` 安装固定版本的 `aliyunpan`，以 `workload-hub` 用户完成OAuth和扫码授权，先手动验证上传再开启定时器。

建议为该VPS配置约1GB交换空间以防偶发内存峰值。应用本身通过 `GOMEMLIMIT=180MiB` 控制Go堆内存，不在服务器上执行编译或前端构建。

## 恢复

常规恢复由系统管理员在网页“备份数据”中上传 `.db` 或 `.db.gz` 完成。系统会校验SQLite完整性和核心表，自动创建恢复前保护备份，恢复后清除全部会话并要求重新登录。

只有网站无法启动时才使用离线恢复：停止服务，复制并保留当前数据库及WAL文件，解压目标备份到 `/var/lib/workload-hub/workload.db`，修正属主与权限后启动服务，再检查 `/healthz`、登录、最近工时及审计日志。

## 节假日

系统初始包含国务院办公厅发布的2026年节假日及调休工作日，系统管理员可以覆盖任意日期。后续年度应在国务院通知发布后更新日历。

来源：中国政府网《国务院办公厅关于2026年部分节假日安排的通知》：https://www.gov.cn/yaowen/liebiao/202511/content_7047099.htm
