-- name: UpsertCurrencyPlaceholder :exec
INSERT INTO currencies (item_path, trade_id, name, emoji_id, is_placeholder)
VALUES (?, ?, ?, NULL, 1)
ON CONFLICT (item_path) DO NOTHING;

-- name: UpsertCurrencyCurated :exec
INSERT INTO currencies (item_path, trade_id, name, emoji_id, is_placeholder, updated_at)
VALUES (?, ?, ?, ?, 0, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
ON CONFLICT (item_path) DO UPDATE SET
    trade_id       = excluded.trade_id,
    name           = excluded.name,
    emoji_id       = excluded.emoji_id,
    is_placeholder = 0,
    updated_at     = excluded.updated_at;

-- name: GetCurrencyByPath :one
SELECT * FROM currencies WHERE item_path = ?;

-- name: ListCurrencies :many
SELECT * FROM currencies ORDER BY name;

-- name: ListPlaceholderCurrencies :many
SELECT * FROM currencies WHERE is_placeholder = 1 ORDER BY discovered_at DESC;
