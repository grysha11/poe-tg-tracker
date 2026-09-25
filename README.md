## Setup

```
cp .env.example .env   # fill in TELGRAM_BOT_TOKEN, POE_LEAGUE, POE_CONTACT, MYSQL_ROOT_PASSWORD, WHITELIST
```

`WHITELIST` is a required comma-separated list of Telegram user IDs (`123,456`); the bot won't start without it and rejects everyone else. To onboard someone: have them message the bot, copy their `user_id` from the `rejected message: user not whitelisted` log line, add it to `WHITELIST` and restart.

`DB_DSN` is auto-set for the `bot`/`exchange-service`/`fetch-cron` containers from `MYSQL_ROOT_PASSWORD`. Only fill it in `.env` yourself if running a binary directly on the host (see below) — use `127.0.0.1:3306` there instead of `mysql:3306`.

`curate` doesn't touch the DB: it calls exchange-service's admin gRPC API at `EXCHANGE_SERVICE_ADDR` (auto-set to `exchange-service:9090` in the `bot` container).

## Run

```
docker compose up --build
```

Starts MySQL + the bot + hourly fetch-cron. MySQL data lives on the `poetracker-mysql-data` volume, schema loads automatically from `internal/db/schema.sql` on first boot.

## Local Kubernetes testing (minikube)

For testing against the real Helm chart without needing the homelab (useful
when it's unreachable, e.g. VPS provider issues). Deploys the chart into a
local minikube cluster, backed by a disposable in-cluster MySQL instead of
the homelab's Vitess vtgate.

Requires `minikube`, `kubectl`, `helm`, `docker`, and [`task`](https://taskfile.dev)
on your `PATH`, plus a filled-in `.env` (same one docker-compose uses).

```
task dev:up        # start minikube, build the image, load it, helm install
task dev:bootstrap # seed the DB (curate sync + bootstrap) — needed after every fresh dev:up
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
GET /v1/rates?league=<league>&view=RATE_VIEW_VOLUME|RATE_VIEW_PRICE&limit=<n>
GET /v1/leagues
GET /v1/rate-pairs/default
GET /healthz   # liveness
GET /readyz    # readiness: exchange-service's gRPC health
```

All params are optional (league defaults to `POE_LEAGUE`, view to volume, limit to 10). 64-bit ints (`hourUtc`, `baseVolume`, …) are JSON strings, per protojson. Admin RPCs (what `curate` uses) are deliberately not exposed here.

No auth yet: the Service is ClusterIP-only and compose binds it to `127.0.0.1`. Add auth before exposing it to anything outside the cluster.

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

On an empty database the bot refuses to start (no default rate pairs). Seed in this order:

```
docker compose run --rm bot ./curate sync
docker compose run --rm bot ./curate bootstrap
```

`bootstrap` seeds `default_rate_pairs` (Divine -> Chaos, Divine -> Exalt) by `item_path`; safe to re-run.

## Tests

Tests truncate all tables before each run — never point `TEST_DB_DSN` at the `mysql` service from `docker compose up` or the minikube dev MySQL (that's your real dev data). Easiest:

```
task test:db       # starts a disposable mysql:8.4 on :3307 if needed, runs go test -p 1 ./...
task test:db-down  # stop and remove it
```

`-p 1` matters: every DB-backed package truncates the same test DB, so packages can't run concurrently.

Or by hand, with a separate, disposable container:

```
docker run -d --rm --name poe-mysql-test -p 3307:3306 \
  -e MYSQL_ROOT_PASSWORD=test -e MYSQL_DATABASE=core \
  -v "$(pwd)/internal/db/schema.sql:/docker-entrypoint-initdb.d/schema.sql:ro" \
  mysql:8.4

export TEST_DB_DSN="root:test@tcp(127.0.0.1:3307)/core?parseTime=false&charset=utf8mb4"
go test -p 1 ./...

docker stop poe-mysql-test   # when done
```

## Schema changes

Edit `internal/db/schema.sql` directly (single desired-state file, no numbered migrations). In prod this is applied via `vtctl ApplySchema` against the Vitess cluster; locally, drop and recreate the `mysql` volume to pick it up, or apply by hand with a `mysql` client.
