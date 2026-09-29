## Setup

```
cp .env.example .env   # fill in TELGRAM_BOT_TOKEN, POE_LEAGUE, POE_CONTACT, MYSQL_ROOT_PASSWORD, WHITELIST
```

`WHITELIST` is a required comma-separated list of Telegram user IDs (`123,456`); the bot won't start without it and rejects everyone else. To onboard someone: have them message the bot, copy their `user_id` from the `rejected message: user not whitelisted` log line, add it to `WHITELIST` and restart.

`DB_DSN` is auto-set for the `migrate`/`exchange-service`/`fetch-cron` containers from `MYSQL_ROOT_PASSWORD`. Only fill it in `.env` yourself if running a binary directly on the host (see below) — use `127.0.0.1:3306` there instead of `mysql:3306`.

The bot doesn't touch the DB: it gets rates through the gateway at `GATEWAY_ADDR` (auto-set to `http://gateway:8080` in compose).

`curate` doesn't touch the DB: it calls exchange-service's admin gRPC API at `EXCHANGE_SERVICE_ADDR` (auto-set to `exchange-service:9090` in the `bot` container).

## Run

```
docker compose up --build
```

Starts MySQL, runs pending migrations (one-shot `migrate` service), then exchange-service, the gateway, the bot and the hourly fetch-cron. MySQL data lives on the `poetracker-mysql-data` volume.

## Local Kubernetes testing (minikube)

For testing against the real Helm chart without needing the homelab (useful
when it's unreachable, e.g. VPS provider issues). Deploys the chart into a
local minikube cluster, backed by a disposable in-cluster MySQL instead of
the homelab's Vitess vtgate.

Requires `minikube`, `kubectl`, `helm`, `docker`, and [`task`](https://taskfile.dev)
on your `PATH`, plus a filled-in `.env` (same one docker-compose uses).

```
task dev:up        # start minikube, build the image, helm install, run pending migrations
task dev:bootstrap # seed the DB (curate sync + bootstrap) — needed after every fresh dev:up
task dev:migrate   # run pending migrations (dev:up and dev:watch already do this)
task dev:fetch-once # optional: pull one hour of real rate data immediately, instead of waiting for the cron
task dev:smoke     # wait for rollout, sanity-check both Deployments are healthy
task dev:logs      # tail the bot's logs
task dev:gateway-forward # expose the gateway on localhost:8080
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

## Gateway (REST API)

`gateway` is the REST/JSON front door for clients (bot today; Discord bot, website, desktop app later). It proxies to exchange-service over gRPC via grpc-gateway, generated from `proto/exchange/v1/query.proto`:

```
GET /v1/rates?league=<league>&view=RATE_VIEW_VOLUME|RATE_VIEW_PRICE&limit=<n>&categories=<category>...
GET /v1/leagues
GET /v1/rate-pairs/default
GET /v1/categories
GET /healthz   # liveness
GET /readyz    # readiness: exchange-service's gRPC health
```

All params are optional (league defaults to `POE_LEAGUE`, view to volume, limit to 10). 64-bit ints (`hourUtc`, `baseVolume`, …) are JSON strings, per protojson. Admin RPCs (what `curate` uses) are deliberately not exposed here.

No auth yet: the Service is ClusterIP-only and compose binds it to `127.0.0.1`. Add auth before exposing it to anything outside the cluster.

## Observability

All binaries go through `internal/telemetry` (OpenTelemetry SDK).

### Logs

JSON on stdout, always, so `kubectl logs` and Argo CD keep working. Every line carries `service` and `service_version` (the image tag). `curate` writes its logs to stderr so its table output stays clean.

When `OTEL_EXPORTER_OTLP_ENDPOINT` is set (chart: `otel.endpoint`, e.g. `http://alloy.<namespace>.svc:4317`), the same records are also exported over OTLP/gRPC to the collector (Loki). Don't let the collector tail these pods' stdout as well, or every line lands twice.

`LOG_LEVEL`: `debug` adds per-request detail (outbound HTTP calls, Telegram API calls, resolved rates, poe2scout pages, health checks, probes). `info` is lifecycle and business events, `warn` is retries and rejected requests, `error` is failed operations.

### Metrics

Prometheus format on `:9464/metrics` (`METRICS_LISTEN_ADDR`) for bot, exchange-service and gateway. The chart creates ServiceMonitors (exchange-service, gateway) and a PodMonitor (bot) when `metrics.serviceMonitor.enabled`; `metrics.serviceMonitor.labels` must match the Prometheus `serviceMonitorSelector` / `podMonitorSelector`:

```
kubectl get prometheus -A -o jsonpath='{range .items[*]}{.spec.serviceMonitorSelector}{"\n"}{end}'
```

| Metric | From |
|---|---|
| `rpc_server_call_duration_seconds` | exchange-service, by `rpc_method` and status code |
| `rpc_client_call_duration_seconds` | gateway → exchange-service |
| `http_server_request_duration_seconds` | gateway REST API (probes excluded) |
| `http_client_request_duration_seconds` | outbound HTTP: bot → gateway, exchange API, poe2scout |
| `db_client_operation_duration_seconds` | every SQL call, `db_query_name` = sqlc query name |
| `db_sql_connection_*` | connection pool |
| `poetracker_last_fetch_timestamp_seconds` | last successful fetcher run, read from `fetch_log` at scrape time |
| `poetracker_latest_snapshot_hour_seconds` | newest snapshot hour for `POE_LEAGUE` |
| `poetracker_placeholder_currencies` | currencies awaiting `curate` |
| `poetracker_scout_sync_currencies_total` | `curate sync` results |
| `poetracker_bot_commands_total`, `poetracker_bot_callbacks_total`, `poetracker_bot_rejected_total`, `poetracker_bot_getupdates_errors_total` | bot usage |
| `poetracker_telegram_requests_total`, `poetracker_telegram_request_duration_seconds` | Telegram Bot API, by method |
| `poetracker_retries_total` | retries by `client` and `outcome` (`retry`, `recovered`, `exhausted`) |

The fetcher is a short-lived CronJob, so it isn't scraped: alert on `time() - poetracker_last_fetch_timestamp_seconds > 7200` instead.

### Probes

- **exchange-service**: readiness is the default gRPC health service, `SERVING` only while a DB ping (every 10s) succeeds. Liveness is the `liveness` health service, which stays up during a DB outage, so the pod leaves the Service instead of restarting.
- **gateway**: `/readyz` checks exchange-service health (and so the DB), `/healthz` is process-only.
- **bot**: on `:9464`. `/healthz` fails only if the poll loop stalls for 3 minutes; a Telegram outage never restarts it. `/readyz` needs a successful `getUpdates` in the last 3 minutes and a ready gateway.

### Retries

- **DB connect**: every binary retries the initial ping for up to `DB_CONNECT_TIMEOUT` (default `60s`), so `migrate` and the services survive MySQL/vtgate starting late.
- **gRPC** (gateway, curate → exchange-service): up to 4 attempts on `UNAVAILABLE`, except `ListLeagues` and `SyncCurrenciesFromScout`, which already retry their upstream. `curate` also waits for the connection to become ready.
- **gateway client** (bot): 3 attempts on network errors and 502/503/504.
- **poe2scout**: 4 attempts per page on network errors, 429 and 5xx, honoring `Retry-After`.
- **exchange API**: 3 attempts in exchange-service (`ListLeagues`); the fetcher keeps its own 6-attempt loop.
- **Telegram**: `getUpdates` backs off exponentially up to 60s. Sends retry only on 429 (honoring `retry_after`) and 5xx, never after a dropped connection, so users never get a message twice.

## Fetcher (manual run)

Normally runs on cron. To trigger a fetch now instead of waiting:

```
docker compose exec bot ./fetcher
```

Flag: `-hour <unix_ts>` to backfill a specific hour. Stale-payload detection (the API sometimes re-serves the previous hour) uses a payload hash stored in the `fetch_log` table, so the fetcher keeps no local state.

## Curate

Manages currency names (raw API only gives internal item paths, not display names) through exchange-service's admin API.

```
docker compose exec bot ./curate list
docker compose exec bot ./curate set -path <item_path> -trade-id <id> -name <name> [-emoji <id>]
```

### Sync from poe2scout

Bulk-upserts real names for all known currencies. Safe to re-run; won't touch emoji ids you've already curated:

```
docker compose exec bot ./curate sync
```

### First-run bootstrap

On an empty database the bot still starts, but `/rates` replies with an error until the DB has default rate pairs and at least one fetched hour. Seed in this order, then run the fetcher once (see above) or wait for the hourly cron:

```
docker compose run --rm bot ./curate sync
docker compose run --rm bot ./curate bootstrap
```

`bootstrap` seeds `default_rate_pairs` (Divine -> Chaos, Divine -> Exalt) by `item_path`; safe to re-run.

## Tests

Tests run the migrations and then truncate all tables before each run — never point `TEST_DB_DSN` at the `mysql` service from `docker compose up` or the minikube dev MySQL (that's your real dev data). Easiest:

```
task test:db       # starts a disposable mysql:8.4 on :3307 if needed, runs go test -p 1 ./...
task test:db-down  # stop and remove it
```

`-p 1` matters: every DB-backed package truncates the same test DB, so packages can't run concurrently.

Or by hand, with a separate, disposable container:

```
docker run -d --rm --name poe-mysql-test -p 3307:3306 \
  -e MYSQL_ROOT_PASSWORD=test -e MYSQL_DATABASE=core \
  mysql:8.4

export TEST_DB_DSN="root:test@tcp(127.0.0.1:3307)/core?parseTime=false&charset=utf8mb4"
go test -p 1 ./...

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

- **Prod (Argo):** the `poe-tracker-migrate` Job is an Argo `PreSync` hook. Every sync runs it with the new image first; if it fails, the sync stops and the old pods keep serving. A successful Job is deleted right away; a failed one is kept until the next sync, so its logs are there: `kubectl -n poe-tracker logs job/poe-tracker-migrate`.
- **docker compose:** the `migrate` service runs before exchange-service on every `up`.
- **minikube:** `task dev:migrate`, which runs at the end of every `helm:dev-install` (so `dev:up` and `dev:watch` too).
- **By hand:** `docker compose run --rm migrate`, or `go run ./cmd/migrate` with `DB_DSN` set.

`00001_baseline.sql` is the schema as it stood when goose was adopted. It uses `CREATE TABLE IF NOT EXISTS`, so on a database created before goose it changes nothing and just records version 1.
