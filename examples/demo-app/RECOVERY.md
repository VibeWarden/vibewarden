# Demo VM — Backup and Recovery

This document covers how to set up daily automated backups of the Kratos
Postgres database on the demo VM and how to restore from a backup.

The backup script lives at `scripts/backup-demo.sh` in this repository.

---

## How backups work

The script:

1. Runs `pg_dump` inside the running `demo-postgres` Docker container.
2. Compresses the dump with `gzip -9` directly into a timestamped file:
   `kratos-backup-YYYY-MM-DD.sql.gz`.
3. Stores the file in a configurable directory (default: `/backups`).
4. Deletes backup files older than 7 days (configurable).
5. Optionally rsyncs the backup directory to a Hetzner Storage Box or any
   rsync-compatible remote, when `RSYNC_TARGET` is set.

---

## Setting up the daily cron job

### 1. Copy the repository to the VM (already done if you used `make deploy-demo`)

The repository is checked out at `~/vibewarden` on the demo VM.

### 2. Create the backup directory

```bash
mkdir -p /backups
```

### 3. Test the script manually

```bash
POSTGRES_USER=kratos \
POSTGRES_DB=kratos \
~/vibewarden/scripts/backup-demo.sh
```

You should see output like:

```
[backup-demo] 2025-06-01T03:00:01Z Starting pg_dump of database 'kratos' from container 'demo-postgres'...
[backup-demo] 2025-06-01T03:00:03Z Backup written: /backups/kratos-backup-2025-06-01.sql.gz (48K)
[backup-demo] 2025-06-01T03:00:03Z Pruning backups older than 7 days...
[backup-demo] 2025-06-01T03:00:03Z RSYNC_TARGET not set — skipping remote copy.
[backup-demo] 2025-06-01T03:00:03Z Backup finished successfully.
```

### 4. Add the cron job

Run `crontab -e` on the VM and add the line below to run backups every day at
03:00 UTC:

```crontab
0 3 * * * POSTGRES_USER=kratos POSTGRES_DB=kratos /root/vibewarden/scripts/backup-demo.sh >> /var/log/backup-demo.log 2>&1
```

To verify the cron job is installed:

```bash
crontab -l
```

### 5. (Optional) Remote copy to Hetzner Storage Box

Set `RSYNC_TARGET` to your Storage Box rsync endpoint and the cron job will
mirror the `/backups` directory after every successful dump:

```crontab
0 3 * * * POSTGRES_USER=kratos POSTGRES_DB=kratos RSYNC_TARGET=u123456@u123456.your-storagebox.de::backups/vibewarden RSYNC_SSH_KEY=/root/.ssh/storagebox_ed25519 /root/vibewarden/scripts/backup-demo.sh >> /var/log/backup-demo.log 2>&1
```

Make sure the SSH key is pre-authorized in the Storage Box settings and that
`ssh -i /root/.ssh/storagebox_ed25519 u123456@u123456.your-storagebox.de` works
without a passphrase before relying on this in cron.

---

## Configuration reference

| Environment variable   | Default                    | Description                                         |
|------------------------|----------------------------|-----------------------------------------------------|
| `BACKUP_DIR`           | `/backups`                 | Directory where `.sql.gz` files are written         |
| `RETAIN_DAYS`          | `7`                        | Days to keep; older files are deleted automatically |
| `POSTGRES_CONTAINER`   | `demo-postgres`            | Name of the running Postgres Docker container       |
| `POSTGRES_USER`        | `kratos`                   | Postgres user passed to `pg_dump`                   |
| `POSTGRES_DB`          | `kratos`                   | Database name to dump                               |
| `RSYNC_TARGET`         | *(empty — skip rsync)*     | rsync destination for remote offsite copy           |
| `RSYNC_SSH_KEY`        | `~/.ssh/id_ed25519`        | SSH key used by rsync (only when target is set)     |

---

## Restoring from a backup

### 1. Identify the backup to restore

```bash
ls -lh /backups/
# kratos-backup-2025-06-01.sql.gz
# kratos-backup-2025-05-31.sql.gz
# ...
```

### 2. Stop services that write to the database

Stop Kratos (and the seed service if it ever restarts) so no writes occur
during the restore:

```bash
cd ~/vibewarden/examples/demo-app
docker compose -f docker-compose.prod.yml stop kratos kratos-migrate seed
```

### 3. Drop and recreate the database

Connect to the running Postgres container and reset the database:

```bash
docker exec -it demo-postgres psql -U kratos -c "DROP DATABASE IF EXISTS kratos;"
docker exec -it demo-postgres psql -U kratos -c "CREATE DATABASE kratos;"
```

### 4. Restore the dump

Decompress and pipe into `psql` inside the container:

```bash
gunzip -c /backups/kratos-backup-2025-06-01.sql.gz \
  | docker exec -i demo-postgres psql -U kratos kratos
```

### 5. Restart the stack

```bash
cd ~/vibewarden/examples/demo-app
docker compose -f docker-compose.prod.yml up -d
```

Wait for the `demo-kratos` container to become healthy, then verify the
application is responding normally.

### 6. Verify

```bash
docker compose -f docker-compose.prod.yml ps
curl -s https://challenge.vibewarden.dev/_vibewarden/health | python3 -m json.tool
```

---

## Log file rotation (optional)

If you redirect cron output to `/var/log/backup-demo.log`, set up logrotate to
keep the log manageable:

```
# /etc/logrotate.d/backup-demo
/var/log/backup-demo.log {
    weekly
    rotate 4
    compress
    missingok
    notifempty
}
```
