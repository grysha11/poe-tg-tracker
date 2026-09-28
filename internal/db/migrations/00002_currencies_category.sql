-- +goose NO TRANSACTION

-- +goose Up
ALTER TABLE currencies ADD COLUMN category VARCHAR(64) NULL, ALGORITHM=INSTANT;

-- +goose Down
ALTER TABLE currencies DROP COLUMN category;
