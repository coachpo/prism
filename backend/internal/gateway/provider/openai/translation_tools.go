package openai

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

const (
	toolSearchProxyName        = "tool_search"
	customToolInputField       = "input"
	chatToolNameMaxLength      = 64
	customToolInputDescription = "Raw string input for the original custom tool. Preserve formatting exactly and follow the original tool definition embedded in the description."
	customToolMetadataHeader   = "Original tool definition:"
)

type ToolKind string

const (
	ToolKindFunction   ToolKind = "function"
	ToolKindNamespace  ToolKind = "namespace"
	ToolKindCustom     ToolKind = "custom"
	ToolKindToolSearch ToolKind = "tool_search"
)

type ToolSpec struct {
	Kind      ToolKind
	Name      string
	Namespace string
}

type ToolContext struct {
	chatTools               []map[string]any
	seenChatNames           map[string]struct{}
	chatNameToSpec          map[string]ToolSpec
	namespaceNameToChatName map[string]string
}

func NewToolContext() *ToolContext {
	return &ToolContext{seenChatNames: map[string]struct{}{}, chatNameToSpec: map[string]ToolSpec{}, namespaceNameToChatName: map[string]string{}}
}

func BuildToolContextFromResponsesPayload(payload map[string]any) *ToolContext {
	context := NewToolContext()
	if tools, _ := payload["tools"].([]any); len(tools) > 0 {
		for _, rawTool := range tools {
			if tool, _ := rawTool.(map[string]any); tool != nil {
				context.AddResponseTool(tool)
			} else if name := strings.TrimSpace(stringValue(rawTool)); name != "" {
				context.AddResponseTool(map[string]any{"type": string(ToolKindCustom), "name": name})
			}
		}
	}
	context.collectToolSearchOutputTools(payload["input"])
	return context
}

func (context *ToolContext) ChatTools() []map[string]any {
	if context == nil {
		return nil
	}
	out := make([]map[string]any, 0, len(context.chatTools))
	for _, tool := range context.chatTools {
		out = append(out, cloneAnyMap(tool))
	}
	return out
}

func (context *ToolContext) LookupChatName(chatName string) (ToolSpec, bool) {
	if context == nil {
		return ToolSpec{}, false
	}
	spec, ok := context.chatNameToSpec[chatName]
	return spec, ok
}

func (context *ToolContext) IsCustomToolChatName(chatName string) bool {
	spec, ok := context.LookupChatName(chatName)
	return ok && spec.Kind == ToolKindCustom
}

func (context *ToolContext) ChatNameForResponseFunction(name string, namespace string) string {
	name = strings.TrimSpace(name)
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		return name
	}
	if context != nil {
		if chatName := context.namespaceNameToChatName[namespaceToolKey(namespace, name)]; chatName != "" {
			return chatName
		}
	}
	return flattenNamespaceToolName(namespace, name)
}

func (context *ToolContext) AddResponseTool(tool map[string]any) {
	if context == nil || tool == nil {
		return
	}
	switch strings.TrimSpace(stringValue(tool["type"])) {
	case string(ToolKindFunction):
		context.addFunctionTool(tool, "")
	case string(ToolKindCustom):
		context.addCustomTool(tool)
	case string(ToolKindToolSearch):
		context.addToolSearchTool()
	case string(ToolKindNamespace):
		context.addNamespaceTool(tool)
	}
}

func (context *ToolContext) addFunctionTool(tool map[string]any, namespace string) {
	originalName := responseToolName(tool)
	if originalName == "" {
		return
	}
	chatName := originalName
	kind := ToolKindFunction
	if strings.TrimSpace(namespace) != "" {
		chatName = flattenNamespaceToolName(namespace, originalName)
		kind = ToolKindNamespace
	}
	chatTool := responseFunctionToolToChatTool(tool, chatName)
	context.addChatTool(chatName, ToolSpec{Kind: kind, Name: originalName, Namespace: namespace}, chatTool)
}

func (context *ToolContext) addCustomTool(tool map[string]any) {
	name := responseToolName(tool)
	if name == "" {
		return
	}
	chatTool := map[string]any{"type": "function", "function": map[string]any{"name": name, "description": responseCustomToolDescription(tool), "parameters": map[string]any{"type": "object", "properties": map[string]any{customToolInputField: map[string]any{"type": "string", "description": customToolInputDescription}}, "required": []any{customToolInputField}}}}
	context.addChatTool(name, ToolSpec{Kind: ToolKindCustom, Name: name}, chatTool)
}

func (context *ToolContext) addToolSearchTool() {
	chatTool := map[string]any{"type": "function", "function": map[string]any{"name": toolSearchProxyName, "description": "Search and load Codex tools, plugins, connectors, and MCP namespaces for the current task.", "parameters": map[string]any{"type": "object", "properties": map[string]any{"query": map[string]any{"type": "string", "description": "Search query for tools or connectors to load."}, "limit": map[string]any{"type": "integer", "description": "Maximum number of tool groups to return."}}, "required": []any{"query"}}}}
	context.addChatTool(toolSearchProxyName, ToolSpec{Kind: ToolKindToolSearch, Name: toolSearchProxyName}, chatTool)
}

func (context *ToolContext) addNamespaceTool(tool map[string]any) {
	namespace := strings.TrimSpace(stringValue(tool["name"]))
	children, _ := tool["tools"].([]any)
	if len(children) == 0 {
		children, _ = tool["children"].([]any)
	}
	for _, rawChild := range children {
		child, _ := rawChild.(map[string]any)
		if child != nil && strings.TrimSpace(stringValue(child["type"])) == string(ToolKindFunction) {
			context.addFunctionTool(child, namespace)
		}
	}
}

func (context *ToolContext) addChatTool(chatName string, spec ToolSpec, chatTool map[string]any) {
	chatName = strings.TrimSpace(chatName)
	if chatName == "" || len(chatTool) == 0 {
		return
	}
	if _, exists := context.seenChatNames[chatName]; exists {
		return
	}
	context.seenChatNames[chatName] = struct{}{}
	context.chatNameToSpec[chatName] = spec
	if spec.Namespace != "" {
		context.namespaceNameToChatName[namespaceToolKey(spec.Namespace, spec.Name)] = chatName
	}
	context.chatTools = append(context.chatTools, chatTool)
}

func (context *ToolContext) collectToolSearchOutputTools(value any) {
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			context.collectToolSearchOutputTools(item)
		}
	case map[string]any:
		if strings.TrimSpace(stringValue(typed["type"])) == "tool_search_output" {
			if tools, _ := typed["tools"].([]any); len(tools) > 0 {
				for _, rawTool := range tools {
					if tool, _ := rawTool.(map[string]any); tool != nil {
						context.AddResponseTool(tool)
					}
				}
			}
		}
		for _, child := range typed {
			context.collectToolSearchOutputTools(child)
		}
	}
}

func responseToolName(tool map[string]any) string {
	if function, _ := tool["function"].(map[string]any); function != nil {
		if name := strings.TrimSpace(stringValue(function["name"])); name != "" {
			return name
		}
	}
	return strings.TrimSpace(stringValue(tool["name"]))
}

func responseFunctionToolToChatTool(tool map[string]any, chatName string) map[string]any {
	if function, _ := tool["function"].(map[string]any); function != nil {
		cloned := cloneAnyMap(function)
		cloned["name"] = chatName
		if _, ok := cloned["strict"]; !ok {
			if strict, ok := tool["strict"]; ok {
				cloned["strict"] = strict
			}
		}
		return map[string]any{"type": "function", "function": cloned}
	}
	function := map[string]any{"name": chatName, "description": tool["description"], "parameters": tool["parameters"]}
	if function["parameters"] == nil {
		function["parameters"] = map[string]any{}
	}
	if strict, ok := tool["strict"]; ok {
		function["strict"] = strict
	}
	return map[string]any{"type": "function", "function": function}
}

func responseCustomToolDescription(tool map[string]any) string {
	return customToolMetadataHeader + "\n```json\n" + canonicalJSONString(tool) + "\n```"
}

func flattenNamespaceToolName(namespace string, name string) string {
	fullName := namespace + "__" + name
	if len(fullName) <= chatToolNameMaxLength {
		return fullName
	}
	hash := shortSHA256Hex(fullName)
	suffix := "__" + hash
	if len(suffix) >= chatToolNameMaxLength {
		return suffix[:chatToolNameMaxLength]
	}
	prefixLimit := chatToolNameMaxLength - len(suffix)
	return fullName[:prefixLimit] + suffix
}

func shortSHA256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:8]
}

func namespaceToolKey(namespace string, name string) string {
	return namespace + "\x00" + name
}

func customToolInputFromChatArguments(arguments string) string {
	object := canonicalToolArgumentsObject(arguments, customToolInputField)
	if input := stringValue(object[customToolInputField]); input != "" {
		return input
	}
	if strings.TrimSpace(arguments) == "" {
		return ""
	}
	return arguments
}

func ResponseToolCallItemFromChatName(itemID string, status string, callID string, chatName string, arguments string, reasoning string, context *ToolContext) map[string]any {
	if context != nil {
		if spec, ok := context.LookupChatName(chatName); ok {
			switch spec.Kind {
			case ToolKindToolSearch:
				item := map[string]any{"type": "tool_search_call", "call_id": callID, "status": status, "execution": "client", "arguments": canonicalToolArgumentsObject(arguments, "query")}
				appendReasoningContentField(item, reasoning)
				return item
			case ToolKindCustom:
				item := map[string]any{"id": itemID, "type": "custom_tool_call", "status": status, "call_id": callID, "name": spec.Name, "input": customToolInputFromChatArguments(arguments)}
				appendReasoningContentField(item, reasoning)
				return item
			case ToolKindNamespace:
				item := map[string]any{"id": itemID, "type": "function_call", "status": status, "call_id": callID, "name": spec.Name, "namespace": spec.Namespace, "arguments": arguments}
				appendReasoningContentField(item, reasoning)
				return item
			}
		}
	}
	item := map[string]any{"id": itemID, "type": "function_call", "status": status, "call_id": callID, "name": chatName, "arguments": arguments}
	appendReasoningContentField(item, reasoning)
	return item
}
