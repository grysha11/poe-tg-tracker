-- +goose Up
INSERT INTO currencies (item_path, trade_id, name, emoji_id, is_placeholder, discovered_at, updated_at) VALUES
    ('Metadata/Items/Currency/CurrencyModValues', 'divine', 'Divine', '5274102213218706909', 0, unixepoch(), unixepoch()),
    ('Metadata/Items/Currency/CurrencyRerollRare', 'chaos', 'Chaos', '5271655413299849333', 0, unixepoch(), unixepoch()),
    ('Metadata/Items/Currency/CurrencyAddModToRare', 'exalted', 'Exalt', '5271469462690770315', 0, unixepoch(), unixepoch());

-- +goose Down
DELETE FROM currencies WHERE item_path IN (
    'Metadata/Items/Currency/CurrencyModValues',
    'Metadata/Items/Currency/CurrencyRerollRare',
    'Metadata/Items/Currency/CurrencyAddModToRare'
);
