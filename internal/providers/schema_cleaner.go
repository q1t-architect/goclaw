package providers

// CleanToolSchemas normalizes tool schemas for a specific provider.
// This is the batch entry point — called from OpenAI/DashScope providers.
func CleanToolSchemas(providerName string, tools []ToolDefinition) []ToolDefinition {
	if len(tools) == 0 {
		return tools
	}
	cleaned := make([]ToolDefinition, len(tools))
	for i, t := range tools {
		cleaned[i] = ToolDefinition{
			Type: t.Type,
			Function: ToolFunctionSchema{
				Name:        t.Function.Name,
				Description: t.Function.Description,
				Parameters:  NormalizeSchema(providerName, t.Function.Parameters),
			},
		}
	}
	return cleaned
}

// CleanSchemaForProvider normalizes a single tool's parameters.
// Called from the Anthropic provider.
func CleanSchemaForProvider(providerName string, params map[string]any) map[string]any {
	return NormalizeSchema(providerName, params)
}
