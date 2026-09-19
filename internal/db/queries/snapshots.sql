-- name: InsertMarketSnapshot :exec
INSERT INTO market_snapshots (
    hour_utc, league, market_id, item_a_id, item_b_id,
    volume_a, volume_b, lowest_ratio_a, lowest_ratio_b, highest_ratio_a, highest_ratio_b,
    fetched_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
    volume_a        = VALUES(volume_a),
    volume_b        = VALUES(volume_b),
    lowest_ratio_a  = VALUES(lowest_ratio_a),
    lowest_ratio_b  = VALUES(lowest_ratio_b),
    highest_ratio_a = VALUES(highest_ratio_a),
    highest_ratio_b = VALUES(highest_ratio_b),
    fetched_at      = VALUES(fetched_at);

-- name: LatestSnapshotHour :one
SELECT CAST(COALESCE(MAX(hour_utc), 0) AS SIGNED) AS hour_utc FROM market_snapshots WHERE league = ?;

-- name: ListSnapshotsForHour :many
SELECT * FROM market_snapshots WHERE hour_utc = ? AND league = ?;
