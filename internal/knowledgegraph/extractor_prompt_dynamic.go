package knowledgegraph

import (
	"fmt"
	"strings"

	"github.com/nextlevelbuilder/goclaw/internal/store"
)

// extractionPromptHeader is reused across both static and dynamic prompts.
const extractionPromptHeader = `You are a knowledge graph extractor for an AI assistant's memory system. Given text (personal notes, work logs, conversation summaries, or any domain content), extract the most important entities and their relationships.

`

// extractionPromptSchema is the JSON output schema section.
const extractionPromptSchema = `Output valid JSON with this schema:
{
  "entities": [
    {
      "external_id": "unique-lowercase-id",
      "name": "Display Name",
      "entity_type": "entity_type_slug",
      "description": "Brief description of the entity",
      "confidence": 0.0-1.0
    }
  ],
  "relations": [
    {
      "source_entity_id": "external_id of source",
      "relation_type": "RELATION_TYPE",
      "target_entity_id": "external_id of target",
      "confidence": 0.0-1.0
    }
  ]
}

`

// extractionPromptIDRules is the entity ID rules section.
const extractionPromptIDRules = `## Entity ID Rules
- Use consistent, canonical lowercase IDs with hyphens
- For people: use full name when known (e.g., "john-doe"), not partial ("john")
- For projects/products: use official name (e.g., "project-alpha", "goclaw")
- Same real-world entity MUST always get the same external_id across extractions
- When a pronoun or partial reference clearly refers to a named entity, use that entity's ID — do NOT create a new entity

`

// extractionPromptRules is the extraction rules section.
const extractionPromptRules = `## Rules
- Extract 3-15 entities depending on text density. Short text = fewer entities
- Confidence: 1.0 = explicitly stated fact, 0.8 = strongly implied, 0.5 = inferred from context
- Use varied confidence — NOT everything is 1.0. Reserve 1.0 for direct, unambiguous statements
- Keep names in original language
- Descriptions: 1 sentence max, capture the entity's role or significance
- Skip generic/vague entities ("the system", "the team" without specific name)
- Do NOT use related_to as a default — if you cannot determine a specific relation, omit it
- Output ONLY the JSON object, no markdown, no code blocks

`

// extractionPromptExample is the example section.
const extractionPromptExample = `## Example

Input: "Talked to Minh about the GoClaw migration. He'll handle the database schema changes by Friday. The team uses PostgreSQL with pgvector. I wrote the migration guide yesterday."

Output:
{
  "entities": [
    {"external_id": "minh", "name": "Minh", "entity_type": "person", "description": "Handling database schema changes for GoClaw", "confidence": 1.0},
    {"external_id": "goclaw", "name": "GoClaw", "entity_type": "project", "description": "Project undergoing migration", "confidence": 1.0},
    {"external_id": "goclaw-migration", "name": "GoClaw Migration", "entity_type": "task", "description": "Database migration task, deadline Friday", "confidence": 1.0},
    {"external_id": "postgresql", "name": "PostgreSQL", "entity_type": "technology", "description": "Database used with pgvector extension", "confidence": 1.0},
    {"external_id": "pgvector", "name": "pgvector", "entity_type": "technology", "description": "PostgreSQL extension for vector embeddings", "confidence": 0.8},
    {"external_id": "migration-guide", "name": "Migration Guide", "entity_type": "document", "description": "Guide for the GoClaw database migration", "confidence": 1.0}
  ],
  "relations": [
    {"source_entity_id": "minh", "relation_type": "assigned_to", "target_entity_id": "goclaw-migration", "confidence": 1.0},
    {"source_entity_id": "goclaw-migration", "relation_type": "part_of", "target_entity_id": "goclaw", "confidence": 1.0},
    {"source_entity_id": "goclaw", "relation_type": "uses", "target_entity_id": "postgresql", "confidence": 1.0},
    {"source_entity_id": "postgresql", "relation_type": "integrates_with", "target_entity_id": "pgvector", "confidence": 0.8},
    {"source_entity_id": "migration-guide", "relation_type": "references", "target_entity_id": "goclaw-migration", "confidence": 1.0}
  ]
}`

// BuildExtractionPrompt generates a dynamic extraction prompt from custom types.
// If both slices are empty, falls back to the default extractionSystemPrompt.
func BuildExtractionPrompt(entityTypes []store.EntityType, relationTypes []store.RelationType) string {
	if len(entityTypes) == 0 && len(relationTypes) == 0 {
		return extractionSystemPrompt
	}

	var sb strings.Builder

	sb.WriteString(extractionPromptHeader)
	sb.WriteString(extractionPromptSchema)
	sb.WriteString(extractionPromptIDRules)

	// Dynamic Entity Types section
	fmt.Fprintf(&sb, "\n## Entity Types (use ONLY these %d)\n", len(entityTypes))
	for _, et := range entityTypes {
		if et.Description != "" {
			fmt.Fprintf(&sb, "- %s: %s\n", et.Name, et.Description)
		} else if et.DisplayName != "" {
			fmt.Fprintf(&sb, "- %s: %s\n", et.Name, et.DisplayName)
		} else {
			fmt.Fprintf(&sb, "- %s\n", et.Name)
		}
	}
	sb.WriteString("\n")

	// Dynamic Relation Types section
	fmt.Fprintf(&sb, "## Relation Types (use ONLY these %d)\n", len(relationTypes))
	for _, rt := range relationTypes {
		if !rt.Directed {
			fmt.Fprintf(&sb, "- %s (bidirectional)", rt.Name)
		} else {
			sb.WriteString("- ")
			sb.WriteString(rt.Name)
		}
		if rt.Description != "" {
			sb.WriteString(": ")
			sb.WriteString(rt.Description)
		}
		sb.WriteString("\n")
	}
	sb.WriteString("\n")

	sb.WriteString(extractionPromptRules)
	sb.WriteString(extractionPromptExample)

	return sb.String()
}
