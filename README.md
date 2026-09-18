## Setup

```
cp .env.example .env   # fill in TELGRAM_BOT_TOKEN, POE_LEAGUE, POE_CONTACT, DB_PATH
```

## Run

```
docker compose up --build
```

Starts the bot + hourly fetch-cron. DB lives on the `poetracker-data` volume.

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

Bulk-generates a migration with real names for all known currencies. Run **locally** (not in the container), review the generated file, commit, then deploy:

```
go run ./cmd/curate sync
```
