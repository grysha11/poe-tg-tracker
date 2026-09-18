-- name: ListSnapshotRatesForHour :many
SELECT
    a.item_path AS item_a_path, a.name AS item_a_name, a.trade_id AS item_a_trade_id,
    b.item_path AS item_b_path, b.name AS item_b_name, b.trade_id AS item_b_trade_id,
    ms.volume_a, ms.volume_b,
    ms.lowest_ratio_a, ms.lowest_ratio_b,
    ms.highest_ratio_a, ms.highest_ratio_b
FROM market_snapshots ms
JOIN currencies a ON a.currency_id = ms.item_a_id
JOIN currencies b ON b.currency_id = ms.item_b_id
WHERE ms.hour_utc = ? AND ms.league = ?
  AND (a.trade_id IN ('divine', 'chaos', 'exalted') OR b.trade_id IN ('divine', 'chaos', 'exalted'));
