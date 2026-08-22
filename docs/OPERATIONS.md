# Production operations

## First deployment

1. Copy `.env.example` to `.env` and replace every placeholder. Generate independent random values for the database passwords, admin password, and token signing key.
2. Put `rclone.conf` in `RCLONE_CONFIG_DIR` and mount upload sources through `LOCAL_DATA_DIR`. Upload sources stay read-only. The dedicated rclone directory is writable because OAuth backends may persist refreshed tokens; restrict it to UID 10001, keep it outside the source tree in serious deployments, and include it in secret backup/rotation procedures. Static-credential remotes may be mounted read-only after verification.
3. Validate configuration with `docker compose config`, then start with `docker compose up -d --build`.
4. Wait for `docker compose ps` to report both services healthy. Startup runs `rclone listremotes --long` and refuses to continue if the binary/config is unreadable or an allowlisted remote is missing. Verify `/api/health/live` and `/api/health/ready`, log in, create a test watch folder, and complete one canary upload.
5. Terminate TLS at a reverse proxy or ingress. The default host bind is loopback so the service is not exposed directly.

The application container is non-root, read-only, and capability-free. It rejects startup when production credentials are absent or `rclone.conf` contains no usable remote; configured remotes are discovered automatically and may optionally be restricted to an explicit subset.

## Backup and restore

Create an online, transactionally consistent full backup:

```sh
docker compose --profile operations run --rm backup
```

The command writes a compressed SQL dump and SHA-256 checksum to `BACKUP_DIR`. The dump records its binary-log position. Copy backups to storage outside this host; a local bind mount alone is not disaster recovery.

For a Linux host, install the example units from `deploy/systemd/`, adjust `WorkingDirectory`, then enable the timer with `systemctl enable --now rclone-sync-hub-backup.timer`. Monitor both the timer result and backup file age.

Run a restore drill against an isolated environment at least monthly. To restore this environment, first stop all writers:

```sh
docker compose stop app
RESTORE_FILE=/backups/rclone_sync_hub_YYYYMMDDTHHMMSSZ.sql.gz ALLOW_RESTORE=YES docker compose --profile operations run --rm restore
docker compose up -d app
```

After restore, check readiness, row counts, the most recent scan/task records, and perform a canary upload. MySQL binary logging is configured with durable flush settings and seven-day retention. For point-in-time recovery, restore the latest full dump and replay retained binlogs after the position embedded in the dump, stopping before the incident timestamp. Practice this procedure on a separate MySQL instance before relying on it.

Suggested objectives:

- RPO: no more than 24 hours from full backups; lower it by scheduling the backup profile more frequently and archiving binlogs off-host.
- RTO: 60 minutes after infrastructure is available; measure this with restore drills.

## Upgrade and rollback

1. Back up the database and retain the previous immutable image tag.
2. Run CI and build the candidate image. Deploy it to staging using a copy of production configuration and representative files.
3. Verify migrations, authentication, scan history, task claiming, retry/cancel, and one real rclone transfer.
4. Roll out one application instance first. Check readiness, error logs, queue age, scan failures, worker failures, and database connection pressure before completing rollout.
5. Roll back the application image if needed. Database migrations are append-only and are not automatically reversed; destructive schema changes require an explicit expand/migrate/contract release plan.

Before this production release, verify that no two existing watch folders are parent/child paths. Startup intentionally fails with both folder IDs if legacy data violates this invariant, so an operator can resolve the ambiguous ownership instead of silently uploading a file to two destinations. Migration 3 automatically cancels unfinished work whose watch folder was already deleted and makes its file snapshots reclaimable.

## Monitoring and alerting

Scrape `/metrics` over the private service network with `Authorization: Bearer <METRICS_BEARER_TOKEN>`. If that dedicated token is omitted, an admin access token is required instead. At minimum, alert on:

- readiness failures for two minutes;
- any sustained scan error rate or no successful scan for more than twice the configured interval;
- growth in pending task age, exhausted retries, or repeated worker failures;
- database pool saturation (`db_in_use_connections` close to the configured maximum) and increasing wait count;
- disk pressure for the MySQL volume, backup destination, and source filesystem;
- missing or failed scheduled backups and checksum failures.

Ready-to-load Prometheus rules are provided in `deploy/prometheus/alerts.yml`. An Nginx TLS/SSE/rate-limit example is provided in `deploy/nginx/rclone-sync-hub.conf.example`; set `TRUSTED_PROXIES` to the proxy address or CIDR only after the proxy overwrites forwarded headers.

Use JSON logs together with the `X-Request-ID` response header and audit logs to correlate mutations. Do not log credentials, bearer tokens, full rclone configuration, or remote provider secrets.

The maintenance loop deletes data in bounded batches. Defaults retain upload logs for 30 days, scan runs for 90 days, audit logs for 180 days, and terminal upload tasks for 365 days. Keep `TASK_RETENTION_DAYS` longer than `UPLOAD_LOG_RETENTION_DAYS`; export records to an external archive first if regulations require a longer history.

## Incident checklist

1. Freeze changes and record the incident time.
2. Check liveness/readiness, recent deployment changes, database health, scan runs, worker task results, and rclone errors.
3. If uploads may be unsafe, pause watch folders or stop the application; durable task leases allow work to resume after restart.
4. Preserve logs, audit records, database backup, and relevant binlogs before cleanup.
5. Recover, run a canary upload, monitor one complete scan interval, then document cause and preventive action.
