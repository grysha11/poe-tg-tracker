-- +goose Up
CREATE TABLE default_rate_pairs (
    base_item_path  TEXT NOT NULL REFERENCES currencies (item_path),
    quote_item_path TEXT NOT NULL REFERENCES currencies (item_path),
    sort_order      INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (base_item_path, quote_item_path)
);

INSERT INTO default_rate_pairs (base_item_path, quote_item_path, sort_order) VALUES
    ('Metadata/Items/Currency/CurrencyModValues', 'Metadata/Items/Currency/CurrencyRerollRare', 0),
    ('Metadata/Items/Currency/CurrencyModValues', 'Metadata/Items/Currency/CurrencyAddModToRare', 1);

-- +goose Down
DROP TABLE default_rate_pairs;
