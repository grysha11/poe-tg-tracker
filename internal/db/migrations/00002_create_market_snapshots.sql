-- +goose Up
CREATE TABLE market_snapshots (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    hour_utc        INTEGER NOT NULL,
    league          TEXT NOT NULL,
    market_id       TEXT NOT NULL,
    item_a_id       INTEGER NOT NULL REFERENCES currencies (currency_id),
    item_b_id       INTEGER NOT NULL REFERENCES currencies (currency_id),
    volume_a        INTEGER NOT NULL,
    volume_b        INTEGER NOT NULL,
    lowest_ratio_a  INTEGER NOT NULL,
    lowest_ratio_b  INTEGER NOT NULL,
    highest_ratio_a INTEGER NOT NULL,
    highest_ratio_b INTEGER NOT NULL,
    fetched_at      INTEGER NOT NULL,
    UNIQUE (hour_utc, league, item_a_id, item_b_id)
);

CREATE INDEX idx_market_snapshots_hour_items ON market_snapshots (hour_utc, item_a_id, item_b_id);

-- +goose Down
DROP TABLE market_snapshots;
