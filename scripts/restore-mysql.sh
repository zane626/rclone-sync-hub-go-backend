#!/bin/sh
set -eu

: "${MYSQL_HOST:?MYSQL_HOST is required}"
: "${MYSQL_PORT:?MYSQL_PORT is required}"
: "${MYSQL_USER:?MYSQL_USER is required}"
: "${MYSQL_PASSWORD:?MYSQL_PASSWORD is required}"

if [ "${ALLOW_RESTORE:-NO}" != "YES" ]; then
  echo "restore refused: set ALLOW_RESTORE=YES after stopping the application" >&2
  exit 2
fi

backup_dir="${BACKUP_DIR:-/backups}"
restore_file="${RESTORE_FILE:-}"
case "$restore_file" in
  "$backup_dir"/rclone_sync_hub_*.sql.gz) ;;
  *) echo "RESTORE_FILE must be an absolute backup path under $backup_dir" >&2; exit 2 ;;
esac
if [ ! -f "$restore_file" ]; then
  echo "backup not found: $restore_file" >&2
  exit 2
fi

archive_name="$(basename "$restore_file")"
checksum_file="$restore_file.sha256"
if [ ! -f "$checksum_file" ]; then
  echo "checksum not found: $checksum_file" >&2
  exit 2
fi
(
  cd "$backup_dir"
  sha256sum -c "$archive_name.sha256"
)

sql_tmp="$(mktemp /tmp/rclone-sync-hub-restore.XXXXXX.sql)"
trap 'rm -f "$sql_tmp"' EXIT HUP INT TERM
gzip -dc "$restore_file" > "$sql_tmp"

export MYSQL_PWD="$MYSQL_PASSWORD"
mysql \
  --host="$MYSQL_HOST" \
  --port="$MYSQL_PORT" \
  --user="$MYSQL_USER" < "$sql_tmp"

echo "restore completed from: $restore_file"
