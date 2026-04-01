-- 002 down: remove source and updated_at from kg_relations
ALTER TABLE kg_relations DROP COLUMN IF EXISTS updated_at;
ALTER TABLE kg_relations DROP COLUMN IF EXISTS source;
