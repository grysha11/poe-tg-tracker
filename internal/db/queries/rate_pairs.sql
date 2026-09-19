-- name: ListDefaultRatePairs :many
SELECT
    rp.base_currency_id  AS base_currency_id,
    rp.quote_currency_id AS quote_currency_id,
    rp.sort_order        AS sort_order,
    base.item_path       AS base_item_path,
    base.name            AS base_name,
    base.trade_id        AS base_trade_id,
    quote.item_path      AS quote_item_path,
    quote.name           AS quote_name,
    quote.trade_id       AS quote_trade_id
FROM default_rate_pairs rp
LEFT JOIN currencies base  ON base.currency_id  = rp.base_currency_id
LEFT JOIN currencies quote ON quote.currency_id = rp.quote_currency_id
ORDER BY rp.sort_order;

-- name: CountOrphanSnapshotCurrencies :one
SELECT COUNT(*) FROM market_snapshots ms
LEFT JOIN currencies a ON a.currency_id = ms.item_a_id
LEFT JOIN currencies b ON b.currency_id = ms.item_b_id
WHERE a.currency_id IS NULL OR b.currency_id IS NULL;

-- name: CountOrphanRatePairCurrencies :one
SELECT COUNT(*) FROM default_rate_pairs rp
LEFT JOIN currencies base  ON base.currency_id  = rp.base_currency_id
LEFT JOIN currencies quote ON quote.currency_id = rp.quote_currency_id
WHERE base.currency_id IS NULL OR quote.currency_id IS NULL;

-- name: UpsertDefaultRatePair :exec
INSERT INTO default_rate_pairs (base_currency_id, quote_currency_id, sort_order)
VALUES (?, ?, ?)
ON DUPLICATE KEY UPDATE sort_order = VALUES(sort_order);
