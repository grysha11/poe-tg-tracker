-- +goose Up
CREATE TABLE currencies (
    currency_id    INTEGER PRIMARY KEY AUTOINCREMENT,
    item_path      TEXT NOT NULL UNIQUE,
    trade_id       TEXT NOT NULL,
    name           TEXT NOT NULL,
    emoji_id       TEXT,
    is_placeholder INTEGER NOT NULL DEFAULT 1,
    discovered_at  INTEGER NOT NULL,
    updated_at     INTEGER NOT NULL
);

CREATE INDEX idx_currencies_trade_id ON currencies (trade_id);

-- +goose Down
DROP TABLE currencies;
