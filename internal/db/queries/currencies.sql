-- name: UpsertCurrencyPlaceholder :exec
INSERT INTO currencies (item_path, trade_id, name, emoji_id, is_placeholder, discovered_at, updated_at)
VALUES (?, ?, ?, NULL, 1, ?, ?)
ON DUPLICATE KEY UPDATE item_path = item_path;

-- name: UpsertCurrencyCurated :exec
INSERT INTO currencies (item_path, trade_id, name, emoji_id, is_placeholder, discovered_at, updated_at)
VALUES (?, ?, ?, ?, 0, ?, ?)
ON DUPLICATE KEY UPDATE
    trade_id       = VALUES(trade_id),
    name           = VALUES(name),
    emoji_id       = VALUES(emoji_id),
    is_placeholder = 0,
    updated_at     = VALUES(updated_at);

-- name: UpsertCurrencySynced :exec
INSERT INTO currencies (item_path, trade_id, name, emoji_id, is_placeholder, discovered_at, updated_at)
VALUES (?, ?, ?, NULL, 0, ?, ?)
ON DUPLICATE KEY UPDATE
    trade_id       = VALUES(trade_id),
    name           = VALUES(name),
    is_placeholder = 0,
    updated_at     = VALUES(updated_at);

-- name: GetCurrencyByPath :one
SELECT * FROM currencies WHERE item_path = ?;

-- name: ListCurrencies :many
SELECT * FROM currencies ORDER BY name;

-- name: ListPlaceholderCurrencies :many
SELECT * FROM currencies WHERE is_placeholder = 1 ORDER BY discovered_at DESC;
