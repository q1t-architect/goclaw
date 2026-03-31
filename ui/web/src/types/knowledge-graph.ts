export interface KGEntity {
  id: string;
  agent_id: string;
  user_id?: string;
  external_id: string;
  name: string;
  entity_type: string;
  description?: string;
  properties?: Record<string, string>;
  source_id?: string;
  confidence: number;
  created_at: number;
  updated_at: number;
}

export interface KGRelation {
  id: string;
  agent_id: string;
  user_id?: string;
  source_entity_id: string;
  relation_type: string;
  target_entity_id: string;
  confidence: number;
  properties?: Record<string, string>;
  created_at: number;
}

export interface KGTraversalResult {
  entity: KGEntity;
  depth: number;
  path: string[];
  via: string;
}

export interface KGStats {
  entity_count: number;
  relation_count: number;
  entity_types: Record<string, number>;
}

export interface KGDedupCandidate {
  id: string;
  entity_a: KGEntity;
  entity_b: KGEntity;
  similarity: number;
  status: string;
  created_at: number;
}

export interface KGEntityType {
  id: string;
  agent_id: string;
  name: string;
  display_name: string;
  color: string;
  icon?: string;
  description: string;
  properties_schema?: KGPropertyField[];
  is_system: boolean;
  sort_order: number;
  created_at: number;
  updated_at: number;
}

export interface KGRelationType {
  id: string;
  agent_id: string;
  name: string;
  display_name: string;
  color: string;
  directed: boolean;
  description: string;
  properties_schema?: KGPropertyField[];
  is_system: boolean;
  sort_order: number;
  created_at: number;
  updated_at: number;
}

export interface KGPropertyField {
  key: string;
  label: string;
  type: "string" | "number" | "date" | "enum";
  required: boolean;
  enum_values?: string[];
}
