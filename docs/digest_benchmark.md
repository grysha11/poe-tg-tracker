# `ingest.Run` benchmark results

Measured on: AMD Ryzen 7 7800X3D (16 logical CPUs), linux/amd64, go1.27.1,
`mattn/go-sqlite3` (cgo driver). Benchmarks live in `internal/ingest/ingest_bench_test.go`,
fixture in `internal/ingest/testdata/real_digest.json`.

## Input data

`real_digest.json` is a live capture from the real PoE2 currency-exchange API
(`go run ./cmd/fetcher`, league `Forbidden Rites`), not synthetic:

- 3,086 markets (`markets_seen`), 0 skipped
- 671 currencies referenced (638 already seeded by migrations, 33 discovered new by
  this fetch)
- 2.5 MB raw JSON

## Running it

```
go test ./internal/ingest -bench=. -benchmem -count=10
```

## Results (`count=10`, `benchstat`)

| Benchmark | time/op | mem/op | allocs/op |
|---|---|---|---|
| `Migrate` | 20.4µs ± 1% | 6.14 KiB | 108 |
| `ListCurrencies` (671 rows) | 589µs ± 2% | 417 KiB | 6,481 |
| `Run_Warm` (steady state, all currencies known) | **27.2ms ± 2%** | 3.25 MiB | 49,868 |
| `Run_Cold` (fresh DB, all 33 new currencies rediscovered) | **27.7ms ± 1%** | 3.31 MiB | 51,822 |

`Run_Warm` is the number that matters for production: a normal hourly re-run once the
currency set has settled. `Run_Cold` is the same digest against a bare freshly-migrated
DB, forcing every one of the 33 real new-currency paths through the placeholder-creation
branch on every iteration — the worst case actually observed from real data.

**Discovering 33 new currencies costs ~0.5ms and ~2k extra allocations** — noise next to
the 27ms baseline. `Migrate` and `ListCurrencies` together cost ~0.6ms, about 2% of a
full run; not worth touching.

## Absolute cost of one real ingest (process-level)

Single real ingest, measured as a standalone process (not amortized across benchmark
iterations):

- Wall clock: ~27-30ms (matches the benchmark's own timed number)
- Peak process RSS: **~39 MB** (includes loading/parsing the 2.5 MB digest JSON, the
  Go runtime, the cgo sqlite driver, and the SQLite DB file)

## Where the time actually goes

Profiled `Run_Warm` in isolation (`-cpuprofile`/`-memprofile`, not mixed with the other
benchmarks):

- **95.6%** of `ingest.Run`'s CPU time is inside `InsertMarketSnapshot` — i.e. almost
  the entire cost is the per-market write loop, not `Migrate`/`ListCurrencies`/resolve.
- Of that, **57.6%** of total CPU time is spent inside
  `SQLiteConn.prepare`/`prepareWithCache` — the exact same `INSERT` statement gets
  re-prepared from scratch on every one of the 3,086 calls instead of being prepared
  once and reused.
- **84-86%** flat CPU time is `runtime.cgocall` — raw Go↔C transition overhead, paid
  once per `ExecContext` call (3,086 times per run) because `go-sqlite3` is a cgo
  driver.
- On the allocation side, `driverArgsConnLocked` (marshaling query args per call) is the
  single biggest flat allocator at 35% of bytes, and `InsertMarketSnapshot`'s subtree
  accounts for 69% of all bytes allocated in a run.

In short: the cost is entirely explained by doing **one individual statement-prepare +
cgo round trip per market row**, 3,086 times, rather than one prepared statement reused
across the loop or a batched multi-row insert.

## Verdict

**Not worth optimizing right now.** `fetcher` runs once an hour via cron with a 60s
context timeout; a 27ms, 39 MB ingest leaves enormous headroom, and new-currency
discovery doesn't meaningfully change that.

If the market/currency volume grows an order of magnitude, or a bulk historical
backfill across many hours is ever added, the profiling above points at exactly one
fix: prepare the `InsertMarketSnapshot` statement once per `Run` call (`tx.PrepareContext`
+ reuse) instead of letting each `ExecContext` re-prepare it — that alone accounts for
over half of the current CPU cost. A batched multi-row `INSERT` would compound that
further by cutting the per-row cgo-call count too.
