package openai

import (
	"encoding/json"
	"sort"
	"strings"
)

type ChatContentPartKind string

const (
	ChatContentPartText       ChatContentPartKind = "text"
	ChatContentPartImageURL   ChatContentPartKind = "image_url"
	ChatContentPartInputAudio ChatContentPartKind = "input_audio"
	ChatContentPartFile       ChatContentPartKind = "file"
)

type ChatContentPart struct {
	Kind       ChatContentPartKind
	Text       string
	ImageURL   map[string]any
	InputAudio map[string]any
	File       map[string]any
}

type ResponsesContentPartKind string

const (
	ResponsesContentPartInputText  ResponsesContentPartKind = "input_text"
	ResponsesContentPartOutputText ResponsesContentPartKind = "output_text"
	ResponsesContentPartText       ResponsesContentPartKind = "text"
	ResponsesContentPartRefusal    ResponsesContentPartKind = "refusal"
	ResponsesContentPartInputImage ResponsesContentPartKind = "input_image"
	ResponsesContentPartInputAudio ResponsesContentPartKind = "input_audio"
	ResponsesContentPartInputFile  ResponsesContentPartKind = "input_file"
)

type ResponsesContentPart struct {
	Kind       ResponsesContentPartKind
	Text       string
	Refusal    string
	ImageURL   map[string]any
	InputAudio map[string]any
	File       map[string]any
}

func ResponsesContentPartsToChatContent(parts []ResponsesContentPart) any {
	chatParts := make([]any, 0, len(parts))
	hasNonText := false
	for _, part := range parts {
		switch part.Kind {
		case ResponsesContentPartInputText, ResponsesContentPartOutputText, ResponsesContentPartText:
			if strings.TrimSpace(part.Text) != "" {
				chatParts = append(chatParts, map[string]any{"type": string(ChatContentPartText), "text": part.Text})
			}
		case ResponsesContentPartRefusal:
			if strings.TrimSpace(part.Refusal) != "" {
				chatParts = append(chatParts, map[string]any{"type": string(ChatContentPartText), "text": part.Refusal})
			}
		case ResponsesContentPartInputImage:
			if len(part.ImageURL) > 0 {
				chatParts = append(chatParts, map[string]any{"type": string(ChatContentPartImageURL), "image_url": cloneAnyMap(part.ImageURL)})
				hasNonText = true
			}
		case ResponsesContentPartInputAudio:
			if len(part.InputAudio) > 0 {
				chatParts = append(chatParts, map[string]any{"type": string(ChatContentPartInputAudio), "input_audio": cloneAnyMap(part.InputAudio)})
				hasNonText = true
			}
		case ResponsesContentPartInputFile:
			if len(part.File) > 0 {
				chatParts = append(chatParts, map[string]any{"type": string(ChatContentPartFile), "file": cloneAnyMap(part.File)})
				hasNonText = true
			}
		}
	}
	if !hasNonText {
		texts := make([]string, 0, len(chatParts))
		for _, raw := range chatParts {
			part, _ := raw.(map[string]any)
			if text := stringValue(part["text"]); text != "" {
				texts = append(texts, text)
			}
		}
		return strings.Join(texts, "\n")
	}
	return chatParts
}

func ChatContentPartsToResponses(parts []ChatContentPart, textKind ResponsesContentPartKind) []ResponsesContentPart {
	responses := make([]ResponsesContentPart, 0, len(parts))
	for _, part := range parts {
		switch part.Kind {
		case ChatContentPartText:
			responses = append(responses, ResponsesContentPart{Kind: textKind, Text: part.Text})
		case ChatContentPartImageURL:
			responses = append(responses, ResponsesContentPart{Kind: ResponsesContentPartInputImage, ImageURL: cloneAnyMap(part.ImageURL)})
		case ChatContentPartInputAudio:
			responses = append(responses, ResponsesContentPart{Kind: ResponsesContentPartInputAudio, InputAudio: cloneAnyMap(part.InputAudio)})
		case ChatContentPartFile:
			responses = append(responses, ResponsesContentPart{Kind: ResponsesContentPartInputFile, File: cloneAnyMap(part.File)})
		}
	}
	return responses
}

func canonicalJSONString(value any) string {
	raw, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func canonicalToolArguments(value any) string {
	switch typed := value.(type) {
	case nil:
		return "{}"
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return "{}"
		}
		var decoded any
		if err := json.Unmarshal([]byte(trimmed), &decoded); err != nil {
			return typed
		}
		return canonicalJSONString(decoded)
	default:
		return canonicalJSONString(typed)
	}
}

func canonicalToolArgumentsObject(arguments string, fallbackKey string) map[string]any {
	trimmed := strings.TrimSpace(arguments)
	if trimmed == "" {
		return map[string]any{}
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(trimmed), &decoded); err == nil {
		return decoded
	}
	return map[string]any{fallbackKey: arguments}
}

func cloneAnyMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	keys := make([]string, 0, len(input))
	for key := range input {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make(map[string]any, len(input))
	for _, key := range keys {
		out[key] = input[key]
	}
	return out
}
