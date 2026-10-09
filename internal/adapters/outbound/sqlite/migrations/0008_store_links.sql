-- Every library source now links its games to its own store (Links), and the store cover
-- providers read those links instead of the copies' external ids. Fill the links of the games
-- already imported from the ids of their copies ("epic:<catalogItemId>" → {"epic": "…"}),
-- keeping any link already set; the oldest copy wins when a game has several from one store.
UPDATE games SET links = json_set(links, '$.epic', (
  SELECT substr(external_id, 6) FROM copies
  WHERE game_id = games.id AND external_id LIKE 'epic:%' AND length(external_id) > 5
  ORDER BY created_at, id LIMIT 1))
WHERE json_extract(links, '$.epic') IS NULL
  AND EXISTS (SELECT 1 FROM copies WHERE game_id = games.id AND external_id LIKE 'epic:%' AND length(external_id) > 5);
UPDATE games SET links = json_set(links, '$.gog', (
  SELECT substr(external_id, 5) FROM copies
  WHERE game_id = games.id AND external_id LIKE 'gog:%' AND length(external_id) > 4
  ORDER BY created_at, id LIMIT 1))
WHERE json_extract(links, '$.gog') IS NULL
  AND EXISTS (SELECT 1 FROM copies WHERE game_id = games.id AND external_id LIKE 'gog:%' AND length(external_id) > 4);
UPDATE games SET links = json_set(links, '$.battlenet', (
  SELECT substr(external_id, 11) FROM copies
  WHERE game_id = games.id AND external_id LIKE 'battlenet:%' AND length(external_id) > 10
  ORDER BY created_at, id LIMIT 1))
WHERE json_extract(links, '$.battlenet') IS NULL
  AND EXISTS (SELECT 1 FROM copies WHERE game_id = games.id AND external_id LIKE 'battlenet:%' AND length(external_id) > 10);
UPDATE games SET links = json_set(links, '$.ea', (
  SELECT substr(external_id, 4) FROM copies
  WHERE game_id = games.id AND external_id LIKE 'ea:%' AND length(external_id) > 3
  ORDER BY created_at, id LIMIT 1))
WHERE json_extract(links, '$.ea') IS NULL
  AND EXISTS (SELECT 1 FROM copies WHERE game_id = games.id AND external_id LIKE 'ea:%' AND length(external_id) > 3);
UPDATE games SET links = json_set(links, '$.ubisoft', (
  SELECT substr(external_id, 9) FROM copies
  WHERE game_id = games.id AND external_id LIKE 'ubisoft:%' AND length(external_id) > 8
  ORDER BY created_at, id LIMIT 1))
WHERE json_extract(links, '$.ubisoft') IS NULL
  AND EXISTS (SELECT 1 FROM copies WHERE game_id = games.id AND external_id LIKE 'ubisoft:%' AND length(external_id) > 8);
UPDATE games SET links = json_set(links, '$.xbox', (
  SELECT substr(external_id, 6) FROM copies
  WHERE game_id = games.id AND external_id LIKE 'xbox:%' AND length(external_id) > 5
  ORDER BY created_at, id LIMIT 1))
WHERE json_extract(links, '$.xbox') IS NULL
  AND EXISTS (SELECT 1 FROM copies WHERE game_id = games.id AND external_id LIKE 'xbox:%' AND length(external_id) > 5);
UPDATE games SET links = json_set(links, '$.psn', (
  SELECT substr(external_id, 5) FROM copies
  WHERE game_id = games.id AND external_id LIKE 'psn:%' AND length(external_id) > 4
  ORDER BY created_at, id LIMIT 1))
WHERE json_extract(links, '$.psn') IS NULL
  AND EXISTS (SELECT 1 FROM copies WHERE game_id = games.id AND external_id LIKE 'psn:%' AND length(external_id) > 4);
UPDATE games SET links = json_set(links, '$.amazon', (
  SELECT substr(external_id, 8) FROM copies
  WHERE game_id = games.id AND external_id LIKE 'amazon:%' AND length(external_id) > 7
  ORDER BY created_at, id LIMIT 1))
WHERE json_extract(links, '$.amazon') IS NULL
  AND EXISTS (SELECT 1 FROM copies WHERE game_id = games.id AND external_id LIKE 'amazon:%' AND length(external_id) > 7);
