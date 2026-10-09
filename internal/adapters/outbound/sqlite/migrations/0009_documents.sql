-- Games (with their copies), sources and providers become JSON documents, so adding a field never
-- needs a schema migration (docs/superpowers/specs/2026-10-09-json-documents-design.md). Tables are
-- altered in place: game_details references games(id) with ON DELETE CASCADE, so dropping games
-- would empty the details cache. Integer flags become JSON booleans with json('true'/'false'),
-- because json_object would write 0/1, which a Go bool cannot decode.

ALTER TABLE games ADD COLUMN doc TEXT NOT NULL DEFAULT '{}';

UPDATE games SET doc = json_object(
  'v', 1,
  'title', title,
  'links', json(links),
  'notes', notes,
  'coverUrl', cover_url,
  'createdAt', created_at,
  'updatedAt', updated_at,
  'copies', json((
    SELECT json_group_array(json_object(
      'id', c.id,
      'kind', c.kind,
      'platform', c.platform,
      'status', c.status,
      'key', c.cd_key,
      'redeemBy', c.redeem_by,
      'origin', c.origin,
      'acquiredOn', c.acquired_on,
      'edition', c.edition,
      'condition', c.condition,
      'location', c.location,
      'barcode', c.barcode,
      'notes', c.notes,
      'sourceId', COALESCE(c.source_id, ''),
      'externalId', c.external_id,
      'createdAt', c.created_at,
      'updatedAt', c.updated_at
    ) ORDER BY c.created_at, c.id)
    FROM copies c
    WHERE c.game_id = games.id
  ))
);

DROP INDEX games_title;
ALTER TABLE games DROP COLUMN title;
ALTER TABLE games DROP COLUMN notes;
ALTER TABLE games DROP COLUMN cover_url;
ALTER TABLE games DROP COLUMN links;
ALTER TABLE games DROP COLUMN created_at;
ALTER TABLE games DROP COLUMN updated_at;
DROP TABLE copies;

ALTER TABLE sources ADD COLUMN doc TEXT NOT NULL DEFAULT '{}';

UPDATE sources SET doc = json_object(
  'v', 1,
  'type', type,
  'name', name,
  'enabled', json(CASE WHEN enabled THEN 'true' ELSE 'false' END),
  'syncIntervalSeconds', sync_interval_seconds,
  'settings', json(settings),
  'lastSync', json(last_sync),
  'createdAt', created_at,
  'updatedAt', updated_at
);

ALTER TABLE sources DROP COLUMN type;
ALTER TABLE sources DROP COLUMN name;
ALTER TABLE sources DROP COLUMN enabled;
ALTER TABLE sources DROP COLUMN sync_interval_seconds;
ALTER TABLE sources DROP COLUMN settings;
ALTER TABLE sources DROP COLUMN last_sync;
ALTER TABLE sources DROP COLUMN created_at;
ALTER TABLE sources DROP COLUMN updated_at;

ALTER TABLE providers ADD COLUMN doc TEXT NOT NULL DEFAULT '{}';

UPDATE providers SET doc = json_object(
  'v', 1,
  'kind', kind,
  'enabled', json(CASE WHEN enabled THEN 'true' ELSE 'false' END),
  'priority', priority,
  'settings', json(settings),
  'updatedAt', updated_at
);

DROP INDEX providers_kind;
ALTER TABLE providers DROP COLUMN kind;
ALTER TABLE providers DROP COLUMN enabled;
ALTER TABLE providers DROP COLUMN priority;
ALTER TABLE providers DROP COLUMN settings;
ALTER TABLE providers DROP COLUMN updated_at;

CREATE INDEX games_title ON games(json_extract(doc, '$.title') COLLATE NOCASE);
CREATE INDEX sources_name ON sources(json_extract(doc, '$.name') COLLATE NOCASE);
CREATE INDEX providers_kind ON providers(json_extract(doc, '$.kind'), json_extract(doc, '$.priority'));
