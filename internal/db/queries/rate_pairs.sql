-- name: ListDefaultRatePairs :many
SELECT
    rp.sort_order   AS sort_order,
    base.item_path  AS base_item_path,
    base.name       AS base_name,
    base.trade_id   AS base_trade_id,
    quote.item_path AS quote_item_path,
    quote.name      AS quote_name,
    quote.trade_id  AS quote_trade_id
FROM default_rate_pairs rp
JOIN currencies base ON base.item_path = rp.base_item_path
JOIN currencies quote ON quote.item_path = rp.quote_item_path
ORDER BY rp.sort_order;
