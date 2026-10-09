-- A game's links to stores ({"steam": "620"}) replace the Steam-only steam_app_id column, so no
-- store is special in the catalog.
ALTER TABLE games ADD COLUMN links TEXT NOT NULL DEFAULT '{}';
UPDATE games SET links = json_object('steam', CAST(steam_app_id AS TEXT)) WHERE steam_app_id > 0;
ALTER TABLE games DROP COLUMN steam_app_id;
