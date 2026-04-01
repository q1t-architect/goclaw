-- 002: Add source and updated_at columns to kg_relations
ALTER TABLE kg_relations ADD COLUMN source TEXT NOT NULL DEFAULT 'extraction';
ALTER TABLE kg_relations ADD COLUMN updated_at TIMESTAMPTZ;
