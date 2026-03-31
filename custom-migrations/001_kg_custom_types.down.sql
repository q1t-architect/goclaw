-- Rollback: 000100_kg_custom_types
DROP FUNCTION IF EXISTS seed_kg_default_types(UUID);
DROP TABLE IF EXISTS kg_relation_types;
DROP TABLE IF EXISTS kg_entity_types;
