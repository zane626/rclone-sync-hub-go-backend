#!/bin/sh
set -eu

: "${MYSQL_HOST:?MYSQL_HOST is required}"
: "${MYSQL_PORT:?MYSQL_PORT is required}"
: "${MYSQL_DATABASE:?MYSQL_DATABASE is required}"
: "${MYSQL_USER:?MYSQL_USER is required}"
: "${MYSQL_PASSWORD:?MYSQL_PASSWORD is required}"

backup_dir="${BACKUP_DIR:-/backups}"
retention_days="${BACKUP_RETENTION_DAYS:-14}"
case "$retention_days" in
  ''|*[!0-9]*) echo "BACKUP_RETENTION_DAYS must be a non-negative integer" >&2; exit 2 ;;
esac

mkdir -p "$backup_dir"
umask 077
timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
archive_name="rclone_sync_hub_${timestamp}.sql.gz"
sql_tmp="$backup_dir/.${archive_name}.sql.tmp"
archive_tmp="$backup_dir/.${archive_name}.tmp"
archive="$backup_dir/$archive_name"

cleanup() {
  rm -f "$sql_tmp" "$archive_tmp"
}
trap cleanup EXIT HUP INT TERM

export MYSQL_PWD="$MYSQL_PASSWORD"
mysqldump \
  --host="$MYSQL_HOST" \
  --port="$MYSQL_PORT" \
  --user="$MYSQL_USER" \
  --single-transaction \
  --quick \
  --routines \
  --events \
  --triggers \
  --hex-blob \
  --set-gtid-purged=OFF \
  --source-data=2 \
  --databases "$MYSQL_DATABASE" > "$sql_tmp"

gzip -c "$sql_tmp" > "$archive_tmp"
mv "$archive_tmp" "$archive"
(
  cd "$backup_dir"
  sha256sum "$archive_name" > "$archive_name.sha256"
)

find "$backup_dir" -type f \( -name 'rclone_sync_hub_*.sql.gz' -o -name 'rclone_sync_hub_*.sql.gz.sha256' \) \
  -mtime "+$retention_days" -delete

echo "backup created: $archive"
