-- Game documents become version 3 (covers per edition, docs/superpowers/specs/2026-10-10-platform-editions-design.md).
-- They are converted when read, so nothing changes here: this migration only makes Open write the
-- usual pre-migration backup before the first version-3 document is saved, since an older Game
-- Vault refuses to read those.
UPDATE games SET doc = doc WHERE 0;
