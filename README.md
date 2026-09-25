## Setup

```
cp .env.example .env   # fill in TELGRAM_BOT_TOKEN, POE_LEAGUE, POE_CONTACT, MYSQL_ROOT_PASSWORD, WHITELIST
```

`WHITELIST` is a required comma-separated list of Telegram user IDs (`123,456`); the bot won't start without it and rejects everyone else. To onboard someone: have them message the bot, copy their `user_id` from the `rejected message: user not whitelisted` log line, add it to `WHITELIST` and restart.

`DB_DSN` is auto-set for the `bot`/`fetch-cron` containers from `MYSQL_ROOT_PASSWORD`. Only fill it in `.env` yourself if running a binary directly on the host (see below) — use `127.0.0.1:3306` there instead of `mysql:3306`.

## Run

```
docker compose up --build
```

Starts MySQL, runs pending migrations (one-shot `migrate` service), then the bot + hourly fetch-cron. MySQL data lives on the `poetracker-mysql-data` volume.

## Local Kubernetes testing (minikube)

For testing against the real Helm chart without needing the homelab (useful
when it's unreachable, e.g. VPS provider issues). Deploys the chart into a
local minikube cluster, backed by a disposable in-cluster MySQL instead of
the homelab's Vitess vtgate.

Requires `minikube`, `kubectl`, `helm`, `docker`, and [`task`](https://taskfile.dev)
on your `PATH`, plus a filled-in `.env` (same one docker-compose uses).

```
task dev:up        # start minikube, build the image, load it, helm install
task dev:bootstrap # migrate + seed the DB (migrate, curate sync, bootstrap) — needed after every fresh dev:up
task dev:fetch-once # optional: pull one hour of real rate data immediately, instead of waiting for the cron
task dev:smoke     # wait for rollout, sanity-check both Deployments are healthy
task dev:logs      # tail the bot's logs
task dev:watch     # rebuild + redeploy automatically on Go source changes
task dev:down      # remove the release, keep the cluster running
task dev:destroy   # tear down the release and the minikube cluster
```

`task dev:bootstrap` is required after every fresh `dev:up`, not just the
first one — the in-cluster MySQL's data is an ephemeral `emptyDir` that's
wiped whenever its pod is recreated (e.g. by `dev:down`), same as the "First
run bootstrap" requirement above applies to a fresh prod DB.

Runs in its own `poe-tracker-dev` namespace and `poetracker` minikube
profile, isolated from anything else on the machine. Uses
`deploy/poe-tracker/values.dev.yaml` (in-cluster MySQL, `pullPolicy: Never`,
no SealedSecrets, a plain Secret rebuilt from `.env` by `task secrets:dev`)
and never touches the homelab or GHCR. See `Taskfile.yml` for the full task
list.

## Fetcher (manual run)

Normally runs on cron. To trigger a fetch now instead of waiting:

```
docker compose exec bot ./fetcher
```

Flag: `-hour <unix_ts>` to backfill a specific hour. Stale-payload detection (the API sometimes re-serves the previous hour) uses a payload hash stored in the `fetch_log` table, so the fetcher keeps no local state.

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

### First-run bootstrap

On an empty database the bot refuses to start (no default rate pairs). Seed in this order:

```
docker compose run --rm bot ./curate sync
docker compose run --rm bot ./curate bootstrap
```

`bootstrap` seeds `default_rate_pairs` (Divine -> Chaos, Divine -> Exalt) by `item_path`; safe to re-run.

## Tests

Tests run the migrations and then truncate all tables before each run — never point `TEST_DB_DSN` at the `mysql` service from `docker compose up` (that's your real dev data). Use a separate, disposable container:

```
docker run -d --rm --name poe-mysql-test -p 3307:3306 \
  -e MYSQL_ROOT_PASSWORD=test -e MYSQL_DATABASE=core \
  mysql:8.4

export TEST_DB_DSN="root:test@tcp(127.0.0.1:3307)/core?parseTime=false&charset=utf8mb4"
go test ./...

docker stop poe-mysql-test   # when done
```

## Schema changes

Migrations are [goose](https://github.com/pressly/goose) SQL files in `internal/db/migrations/`, embedded into the `migrate` binary so each image carries exactly the schema its code expects. sqlc reads the same directory, so `sqlc generate` always sees the current schema. Never edit a migration that has already been deployed; add a new one:

```
go install github.com/pressly/goose/v3/cmd/goose@latest   # only needed for `goose create`
goose -dir internal/db/migrations create <name> sql -s
sqlc generate
```

Start each file with `-- +goose NO TRANSACTION` (MySQL DDL auto-commits anyway) and prefer `ALGORITHM=INSTANT` for column adds. `SELECT *` in `internal/db/queries` is safe across column adds: sqlc expands it into an explicit column list at generate time, so already-running pods keep selecting only the columns they know. Hand-written `SELECT *` outside sqlc would not be; keep it out of Go code.

Where it runs:

- **Prod (Argo):** the `poe-tracker-migrate` Job is an Argo `PreSync` hook. Every sync runs it with the new image first; if it fails, the sync stops and the old pods keep serving. Logs: `kubectl -n poe-tracker logs job/poe-tracker-migrate`.
- **docker compose:** the `migrate` service runs before the bot on every `up`.
- **minikube:** `task dev:bootstrap`.
- **By hand:** `docker compose run --rm migrate`, or `go run ./cmd/migrate` with `DB_DSN` set.

`00001_baseline.sql` is the schema as it stood when goose was adopted. It uses `CREATE TABLE IF NOT EXISTS`, so on a database created before goose it changes nothing and just records version 1.
