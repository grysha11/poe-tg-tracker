-- +goose NO TRANSACTION
-- Baseline: the schema as it stood when goose was adopted. IF NOT EXISTS makes
-- this a no-op on databases created from the old schema.sql (goose just
-- records version 1), and creates everything on an empty one.

-- +goose Up
CREATE TABLE IF NOT EXISTS currencies (
  currency_id    BIGINT NOT NULL AUTO_INCREMENT,
  item_path      VARCHAR(255) NOT NULL,
  trade_id       VARCHAR(128) NOT NULL,
  name           VARCHAR(255) NOT NULL,
  emoji_id       VARCHAR(64) NULL,
  is_placeholder TINYINT(1) NOT NULL DEFAULT 1,
  discovered_at  BIGINT NOT NULL,
  updated_at     BIGINT NOT NULL,
  PRIMARY KEY (currency_id),
  UNIQUE KEY uq_currencies_item_path (item_path),
  KEY idx_currencies_trade_id (trade_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS market_snapshots (
  id              BIGINT NOT NULL AUTO_INCREMENT,
  hour_utc        BIGINT NOT NULL,
  league          VARCHAR(128) NOT NULL,
  market_id       VARCHAR(255) NOT NULL,
  item_a_id       BIGINT NOT NULL,
  item_b_id       BIGINT NOT NULL,
  volume_a        BIGINT NOT NULL,
  volume_b        BIGINT NOT NULL,
  lowest_ratio_a  BIGINT NOT NULL,
  lowest_ratio_b  BIGINT NOT NULL,
  highest_ratio_a BIGINT NOT NULL,
  highest_ratio_b BIGINT NOT NULL,
  fetched_at      BIGINT NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uq_snapshot (hour_utc, league, item_a_id, item_b_id),
  KEY idx_snapshots_league_hour (league, hour_utc)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS default_rate_pairs (
  base_currency_id  BIGINT NOT NULL,
  quote_currency_id BIGINT NOT NULL,
  sort_order        BIGINT NOT NULL DEFAULT 0,
  PRIMARY KEY (base_currency_id, quote_currency_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS fetch_log (
  hour_utc       BIGINT NOT NULL,
  payload_sha256 CHAR(64) NOT NULL,
  fetched_at     BIGINT NOT NULL,
  PRIMARY KEY (hour_utc)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- +goose Down
-- Intentionally empty: never drop the baseline.
