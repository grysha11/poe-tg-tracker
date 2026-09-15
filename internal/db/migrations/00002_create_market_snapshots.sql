-- +goose Up
CREATE TABLE market_snapshots (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    hour_utc        INTEGER NOT NULL,
    league          TEXT NOT NULL,
    market_id       TEXT NOT NULL,
    item_a_path     TEXT NOT NULL REFERENCES currencies (item_path),
    item_b_path     TEXT NOT NULL REFERENCES currencies (item_path),
    volume_a        INTEGER NOT NULL,
    volume_b        INTEGER NOT NULL,
    lowest_ratio_a  INTEGER NOT NULL,
    lowest_ratio_b  INTEGER NOT NULL,
    highest_ratio_a INTEGER NOT NULL,
    highest_ratio_b INTEGER NOT NULL,
    fetched_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

CREATE UNIQUE INDEX idx_market_snapshots_hour_market ON market_snapshots (hour_utc, market_id);
CREATE INDEX idx_market_snapshots_hour_items ON market_snapshots (hour_utc, item_a_path, item_b_path);

-- +goose Down
DROP TABLE market_snapshots;
