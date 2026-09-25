-- +goose NO TRANSACTION
-- poe2scout category api id (e.g. "lineagesupportgems"), filled by
-- `curate sync`. NULL for currencies the fetcher discovered that poe2scout
-- doesn't list (yet).

-- +goose Up
ALTER TABLE currencies ADD COLUMN category VARCHAR(64) NULL, ALGORITHM=INSTANT;

-- +goose Down
ALTER TABLE currencies DROP COLUMN category;
