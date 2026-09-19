## Setup

```
cp .env.example .env   # fill in TELGRAM_BOT_TOKEN, POE_LEAGUE, POE_CONTACT, MYSQL_ROOT_PASSWORD
```

`DB_DSN` is auto-set for the `bot`/`fetch-cron` containers from `MYSQL_ROOT_PASSWORD`. Only fill it in `.env` yourself if running a binary directly on the host (see below) — use `127.0.0.1:3306` there instead of `mysql:3306`.

## Run

```
docker compose up --build
```

Starts MySQL + the bot + hourly fetch-cron. MySQL data lives on the `poetracker-mysql-data` volume, schema loads automatically from `internal/db/schema.sql` on first boot.

## Fetcher (manual run)

Normally runs on cron. To trigger a fetch now instead of waiting:

```
docker compose exec bot ./fetcher -out /data/last_fetch.json
```

Flags: `-hour <unix_ts>` to backfill a specific hour, `-out <path>` for output file.

## Curate

Manages currency names in the DB (raw API only gives internal item paths, not display names).

```
docker compose exec bot ./curate list
docker compose exec bot ./curate set -path <item_path> -trade-id <id> -name <name> [-emoji <id>]
```

### Sync from poe2scout

Bulk-upserts real names for all known currencies straight into the DB. Safe to re-run; won't touch emoji ids you've already curated:

```
docker compose exec bot ./curate sync
```

## Tests

Tests truncate all tables before each run — never point `TEST_DB_DSN` at the `mysql` service from `docker compose up` (that's your real dev data). Use a separate, disposable container:

```
docker run -d --rm --name poe-mysql-test -p 3307:3306 \
  -e MYSQL_ROOT_PASSWORD=test -e MYSQL_DATABASE=core \
  -v "$(pwd)/internal/db/schema.sql:/docker-entrypoint-initdb.d/schema.sql:ro" \
  mysql:8.4

export TEST_DB_DSN="root:test@tcp(127.0.0.1:3307)/core?parseTime=false&charset=utf8mb4"
go test ./...

docker stop poe-mysql-test   # when done
```

## Schema changes

Edit `internal/db/schema.sql` directly (single desired-state file, no numbered migrations). In prod this is applied via `vtctl ApplySchema` against the Vitess cluster; locally, drop and recreate the `mysql` volume to pick it up, or apply by hand with a `mysql` client.
