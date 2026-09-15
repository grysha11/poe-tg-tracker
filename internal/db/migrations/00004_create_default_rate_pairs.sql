-- +goose Up
CREATE TABLE default_rate_pairs (
    base_currency_id  INTEGER NOT NULL REFERENCES currencies (currency_id),
    quote_currency_id INTEGER NOT NULL REFERENCES currencies (currency_id),
    sort_order        INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (base_currency_id, quote_currency_id)
);

INSERT INTO default_rate_pairs (base_currency_id, quote_currency_id, sort_order)
SELECT
    (SELECT currency_id FROM currencies WHERE item_path = 'Metadata/Items/Currency/CurrencyModValues'),
    (SELECT currency_id FROM currencies WHERE item_path = 'Metadata/Items/Currency/CurrencyRerollRare'),
    0
UNION ALL
SELECT
    (SELECT currency_id FROM currencies WHERE item_path = 'Metadata/Items/Currency/CurrencyModValues'),
    (SELECT currency_id FROM currencies WHERE item_path = 'Metadata/Items/Currency/CurrencyAddModToRare'),
    1;

-- +goose Down
DROP TABLE default_rate_pairs;
