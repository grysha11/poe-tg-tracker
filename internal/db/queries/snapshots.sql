-- name: InsertMarketSnapshot :exec
INSERT INTO market_snapshots (
    hour_utc, league, market_id, item_a_id, item_b_id,
    volume_a, volume_b, lowest_ratio_a, lowest_ratio_b, highest_ratio_a, highest_ratio_b,
    fetched_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (hour_utc, league, item_a_id, item_b_id) DO UPDATE SET
    volume_a        = excluded.volume_a,
    volume_b        = excluded.volume_b,
    lowest_ratio_a  = excluded.lowest_ratio_a,
    lowest_ratio_b  = excluded.lowest_ratio_b,
    highest_ratio_a = excluded.highest_ratio_a,
    highest_ratio_b = excluded.highest_ratio_b,
    fetched_at      = excluded.fetched_at;

-- name: LatestSnapshotHour :one
SELECT MAX(hour_utc) AS hour_utc FROM market_snapshots WHERE league = ?;

-- name: ListSnapshotsForHour :many
SELECT * FROM market_snapshots WHERE hour_utc = ? AND league = ?;
