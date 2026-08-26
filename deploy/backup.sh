#!/usr/bin/env bash
set -euo pipefail

if [[ -r /etc/workload-hub.env ]]; then
    set -a
    source /etc/workload-hub.env
    set +a
fi

backup_dir="/var/backups/workload-hub"
timestamp="$(date +%Y%m%d-%H%M%S)"
archive="$backup_dir/workload-$timestamp.db.gz"
mkdir -p "$backup_dir"

/opt/workload-hub/workload-hub backup --output "${archive%.gz}"
gzip "${archive%.gz}"

cloud_exit=0
if [[ "${APP_ALIDRIVE_ENABLED:-false}" == "true" ]]; then
    aliyunpan_bin="${APP_ALIDRIVE_CLI_BIN:-/usr/local/bin/aliyunpan}"
    aliyunpan_config="${APP_ALIDRIVE_CONFIG_DIR:-/var/lib/workload-hub/aliyunpan}"
    remote_dir="${APP_ALIDRIVE_PATH:-/载衡人力负荷备份}"
    if [[ ! -x "$aliyunpan_bin" ]]; then
        echo "阿里云盘同步失败：未找到aliyunpan可执行文件 $aliyunpan_bin" >&2
        cloud_exit=20
    elif [[ ! -d "$aliyunpan_config" ]]; then
        echo "阿里云盘同步失败：授权配置目录不存在 $aliyunpan_config" >&2
        cloud_exit=21
    else
        export ALIYUNPAN_CONFIG_DIR="$aliyunpan_config"
        export ALIYUNPAN_VERBOSE=0
        echo "开始同步到阿里云盘：${remote_dir}/$(basename "$archive")"
        if ! "$aliyunpan_bin" upload "$archive" "$remote_dir"; then
            echo "阿里云盘同步失败；VPS本地备份已保留，systemd会记录本次失败。" >&2
            cloud_exit=22
        fi
    fi
fi

mapfile -t backup_files < <(find "$backup_dir" -maxdepth 1 -type f -name 'workload-*.db.gz' -printf '%T@ %p
' | sort -nr | cut -d' ' -f2-)
if (( ${#backup_files[@]} > 3 )); then
    for old_backup in "${backup_files[@]:3}"; do
        if [[ "$old_backup" == "$backup_dir"/workload-*.db.gz ]]; then
            rm -f -- "$old_backup"
        fi
    done
fi

exit "$cloud_exit"
