-- Migration: 000100_kg_custom_types
-- Custom entity and relation types for Knowledge Graph

-- Table: custom entity types
CREATE TABLE kg_entity_types (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id    UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    name        VARCHAR(100) NOT NULL,
    display_name TEXT NOT NULL DEFAULT '',
    color       VARCHAR(7) NOT NULL DEFAULT '#9ca3af',
    icon        VARCHAR(50) NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    properties_schema JSONB DEFAULT '[]',
    is_system   BOOLEAN NOT NULL DEFAULT false,
    sort_order  INT NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(agent_id, name)
);

CREATE INDEX idx_kg_entity_types_agent ON kg_entity_types(agent_id);

-- Table: custom relation types
CREATE TABLE kg_relation_types (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id    UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    name        VARCHAR(200) NOT NULL,
    display_name TEXT NOT NULL DEFAULT '',
    color       VARCHAR(7) NOT NULL DEFAULT '#64748b',
    directed    BOOLEAN NOT NULL DEFAULT true,
    description TEXT NOT NULL DEFAULT '',
    properties_schema JSONB DEFAULT '[]',
    is_system   BOOLEAN NOT NULL DEFAULT false,
    sort_order  INT NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(agent_id, name)
);

CREATE INDEX idx_kg_relation_types_agent ON kg_relation_types(agent_id);

-- Seed function: inserts default types (10 entity + 22 relation) for a given agent
CREATE OR REPLACE FUNCTION seed_kg_default_types(p_agent_id UUID) RETURNS VOID AS $$
BEGIN
    -- Entity types (10)
    INSERT INTO kg_entity_types (agent_id, name, display_name, color, icon, description, is_system, sort_order) VALUES
    (p_agent_id, 'person',       'Person',       '#E85D24', 'user',     'Named individuals',                   true, 1),
    (p_agent_id, 'organization', 'Organization', '#ef4444', 'building', 'Companies, teams, departments',        true, 2),
    (p_agent_id, 'project',      'Project',      '#22c55e', 'folder',   'Initiatives being built',              true, 3),
    (p_agent_id, 'product',      'Product',      '#f97316', 'box',      'Goods, services, platforms',           true, 4),
    (p_agent_id, 'technology',   'Technology',   '#3b82f6', 'cpu',      'Software, tools, frameworks',          true, 5),
    (p_agent_id, 'task',         'Task',         '#f59e0b', 'check',    'Specific work items',                  true, 6),
    (p_agent_id, 'event',        'Event',        '#ec4899', 'calendar', 'Meetings, deadlines, incidents',       true, 7),
    (p_agent_id, 'document',     'Document',     '#8b5cf6', 'file',     'Articles, reports, contracts',         true, 8),
    (p_agent_id, 'concept',      'Concept',      '#a78bfa', 'lightbulb','Abstract ideas, methodologies',        true, 9),
    (p_agent_id, 'location',     'Location',     '#14b8a6', 'map-pin',  'Cities, offices, regions',             true, 10);

    -- Relation types (22)
    INSERT INTO kg_relation_types (agent_id, name, display_name, color, directed, description, is_system, sort_order) VALUES
    (p_agent_id, 'works_on',          'Works on',          '#64748b', true,  'Person actively working on',          true, 1),
    (p_agent_id, 'manages',           'Manages',           '#64748b', true,  'Person managing/leading',             true, 2),
    (p_agent_id, 'reports_to',        'Reports to',        '#64748b', true,  'Reporting relationship',              true, 3),
    (p_agent_id, 'collaborates_with', 'Collaborates with', '#64748b', false, 'Working together',                    true, 4),
    (p_agent_id, 'belongs_to',        'Belongs to',        '#64748b', true,  'Membership/ownership',                true, 5),
    (p_agent_id, 'part_of',           'Part of',           '#64748b', true,  'Component/hierarchical',              true, 6),
    (p_agent_id, 'depends_on',        'Depends on',        '#64748b', true,  'Dependency',                          true, 7),
    (p_agent_id, 'blocks',            'Blocks',            '#64748b', true,  'Blocking dependency',                 true, 8),
    (p_agent_id, 'created',           'Created',           '#64748b', true,  'Authorship/creation',                 true, 9),
    (p_agent_id, 'completed',         'Completed',         '#64748b', true,  'Completion relationship',             true, 10),
    (p_agent_id, 'assigned_to',       'Assigned to',       '#64748b', true,  'Task/person assignment',              true, 11),
    (p_agent_id, 'scheduled_for',     'Scheduled for',     '#64748b', true,  'Scheduling',                          true, 12),
    (p_agent_id, 'located_in',        'Located in',        '#64748b', true,  'Physical location',                   true, 13),
    (p_agent_id, 'based_at',          'Based at',          '#64748b', true,  'Base location',                       true, 14),
    (p_agent_id, 'uses',              'Uses',              '#64748b', true,  'Technology usage',                    true, 15),
    (p_agent_id, 'implements',        'Implements',        '#64748b', true,  'Implementation',                      true, 16),
    (p_agent_id, 'integrates_with',   'Integrates with',   '#64748b', false, 'Integration',                         true, 17),
    (p_agent_id, 'authored',          'Authored',          '#64748b', true,  'Document authorship',                 true, 18),
    (p_agent_id, 'references',        'References',        '#64748b', true,  'Reference link',                      true, 19),
    (p_agent_id, 'provides',          'Provides',          '#64748b', true,  'Capability provided',                 true, 20),
    (p_agent_id, 'requires',          'Requires',          '#64748b', true,  'Capability required',                 true, 21),
    (p_agent_id, 'related_to',        'Related to',        '#64748b', false, 'Generic relation (last resort)',       true, 22);
END;
$$ LANGUAGE plpgsql;

-- Backfill: seed default types for all existing agents
-- Entity types
INSERT INTO kg_entity_types (agent_id, name, display_name, color, icon, description, is_system, sort_order)
SELECT a.id, et.name, et.display_name, et.color, et.icon, et.description, et.is_system, et.sort_order
FROM agents a
CROSS JOIN (
    VALUES ('person',       'Person',       '#E85D24'::VARCHAR(7), 'user'::VARCHAR(50),     'Named individuals'::TEXT,            true, 1),
           ('organization', 'Organization', '#ef4444',             'building',             'Companies, teams, departments',      true, 2),
           ('project',      'Project',      '#22c55e',             'folder',               'Initiatives being built',            true, 3),
           ('product',      'Product',      '#f97316',             'box',                  'Goods, services, platforms',         true, 4),
           ('technology',   'Technology',   '#3b82f6',             'cpu',                  'Software, tools, frameworks',        true, 5),
           ('task',         'Task',         '#f59e0b',             'check',                'Specific work items',                true, 6),
           ('event',        'Event',        '#ec4899',             'calendar',             'Meetings, deadlines, incidents',     true, 7),
           ('document',     'Document',     '#8b5cf6',             'file',                 'Articles, reports, contracts',       true, 8),
           ('concept',      'Concept',      '#a78bfa',             'lightbulb',            'Abstract ideas, methodologies',      true, 9),
           ('location',     'Location',     '#14b8a6',             'map-pin',              'Cities, offices, regions',           true, 10)
) AS et(name, display_name, color, icon, description, is_system, sort_order)
ON CONFLICT (agent_id, name) DO NOTHING;

-- Relation types
INSERT INTO kg_relation_types (agent_id, name, display_name, color, directed, description, is_system, sort_order)
SELECT a.id, rt.name, rt.display_name, rt.color, rt.directed, rt.description, rt.is_system, rt.sort_order
FROM agents a
CROSS JOIN (
    VALUES ('works_on',          'Works on',          '#64748b'::VARCHAR(7), true,     'Person actively working on'::TEXT,   true, 1),
           ('manages',           'Manages',           '#64748b',             true,     'Person managing/leading',            true, 2),
           ('reports_to',        'Reports to',        '#64748b',             true,     'Reporting relationship',             true, 3),
           ('collaborates_with', 'Collaborates with', '#64748b',             false,    'Working together',                   true, 4),
           ('belongs_to',        'Belongs to',        '#64748b',             true,     'Membership/ownership',               true, 5),
           ('part_of',           'Part of',           '#64748b',             true,     'Component/hierarchical',             true, 6),
           ('depends_on',        'Depends on',        '#64748b',             true,     'Dependency',                         true, 7),
           ('blocks',            'Blocks',            '#64748b',             true,     'Blocking dependency',                true, 8),
           ('created',           'Created',           '#64748b',             true,     'Authorship/creation',                true, 9),
           ('completed',         'Completed',         '#64748b',             true,     'Completion relationship',            true, 10),
           ('assigned_to',       'Assigned to',       '#64748b',             true,     'Task/person assignment',             true, 11),
           ('scheduled_for',     'Scheduled for',     '#64748b',             true,     'Scheduling',                         true, 12),
           ('located_in',        'Located in',        '#64748b',             true,     'Physical location',                  true, 13),
           ('based_at',          'Based at',          '#64748b',             true,     'Base location',                      true, 14),
           ('uses',              'Uses',              '#64748b',             true,     'Technology usage',                   true, 15),
           ('implements',        'Implements',        '#64748b',             true,     'Implementation',                     true, 16),
           ('integrates_with',   'Integrates with',   '#64748b',             false,    'Integration',                        true, 17),
           ('authored',          'Authored',          '#64748b',             true,     'Document authorship',                true, 18),
           ('references',        'References',        '#64748b',             true,     'Reference link',                     true, 19),
           ('provides',          'Provides',          '#64748b',             true,     'Capability provided',                true, 20),
           ('requires',          'Requires',          '#64748b',             true,     'Capability required',                true, 21),
           ('related_to',        'Related to',        '#64748b',             false,    'Generic relation (last resort)',      true, 22)
) AS rt(name, display_name, color, directed, description, is_system, sort_order)
ON CONFLICT (agent_id, name) DO NOTHING;
