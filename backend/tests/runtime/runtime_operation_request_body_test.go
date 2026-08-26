package runtimetest

func chatCompletionsBody(modelID string, content string) map[string]any {
	return map[string]any{"messages": []map[string]any{{"role": "user", "content": content}}, "model": modelID}
}

func anthropicMessagesBody(modelID string, content string, maxTokens int) map[string]any {
	return map[string]any{"model": modelID, "messages": []map[string]any{{"role": "user", "content": content}}, "max_tokens": maxTokens}
}
