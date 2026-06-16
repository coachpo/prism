package openai

import "strings"

const (
	thinkOpenTag  = "<think>"
	thinkCloseTag = "</think>"
)

type ReasoningText struct {
	Text string
}

func ExtractReasoningFieldText(value map[string]any) *ReasoningText {
	for _, key := range []string{"reasoning_content", "reasoning"} {
		if text := strings.TrimSpace(stringValue(value[key])); text != "" {
			return &ReasoningText{Text: text}
		}
	}
	if reasoning, _ := value["reasoning"].(map[string]any); reasoning != nil {
		for _, key := range []string{"content", "text", "summary"} {
			if text := strings.TrimSpace(stringValue(reasoning[key])); text != "" {
				return &ReasoningText{Text: text}
			}
		}
	}
	return extractReasoningDetailsText(value["reasoning_details"])
}

func extractReasoningDetailsText(value any) *ReasoningText {
	switch typed := value.(type) {
	case string:
		if trimmed := strings.TrimSpace(typed); trimmed != "" {
			return &ReasoningText{Text: trimmed}
		}
	case []any:
		parts := make([]string, 0, len(typed))
		for _, raw := range typed {
			if text := extractReasoningDetailsText(raw); text != nil {
				parts = append(parts, text.Text)
			}
		}
		if len(parts) > 0 {
			return &ReasoningText{Text: strings.Join(parts, "\n\n")}
		}
	case map[string]any:
		for _, key := range []string{"text", "content", "summary"} {
			if text := strings.TrimSpace(stringValue(typed[key])); text != "" {
				return &ReasoningText{Text: text}
			}
		}
		if parts, _ := typed["parts"].([]any); len(parts) > 0 {
			return extractReasoningDetailsText(parts)
		}
	}
	return nil
}

func ExtractReasoningSummaryText(value map[string]any) *ReasoningText {
	for _, key := range []string{"reasoning_content", "content", "text"} {
		if text := strings.TrimSpace(stringValue(value[key])); text != "" {
			return &ReasoningText{Text: text}
		}
	}
	switch summary := value["summary"].(type) {
	case string:
		if trimmed := strings.TrimSpace(summary); trimmed != "" {
			return &ReasoningText{Text: trimmed}
		}
	case []any:
		parts := make([]string, 0, len(summary))
		for _, raw := range summary {
			if part, _ := raw.(map[string]any); part != nil {
				if text := firstNonEmptyString(part["text"], part["content"]); text != "" {
					parts = append(parts, text)
				}
				continue
			}
			if text := strings.TrimSpace(stringValue(raw)); text != "" {
				parts = append(parts, text)
			}
		}
		if len(parts) > 0 {
			return &ReasoningText{Text: strings.Join(parts, "\n\n")}
		}
	}
	return nil
}

func splitLeadingThinkBlock(text string) (string, string, bool) {
	leadingWhitespaceLength := len(text) - len(strings.TrimLeft(text, "\r\n\t "))
	afterWhitespace := text[leadingWhitespaceLength:]
	if !strings.HasPrefix(afterWhitespace, thinkOpenTag) {
		return "", "", false
	}
	bodyStart := leadingWhitespaceLength + len(thinkOpenTag)
	closeRelative := strings.Index(text[bodyStart:], thinkCloseTag)
	if closeRelative < 0 {
		return "", "", false
	}
	closeStart := bodyStart + closeRelative
	answerStart := closeStart + len(thinkCloseTag)
	return strings.TrimSpace(text[bodyStart:closeStart]), stripThinkAnswerSeparator(text[answerStart:]), true
}

func stripLeadingThinkOpenTag(text string) (string, bool) {
	leadingWhitespaceLength := len(text) - len(strings.TrimLeft(text, "\r\n\t "))
	afterWhitespace := text[leadingWhitespaceLength:]
	if !strings.HasPrefix(afterWhitespace, thinkOpenTag) {
		return "", false
	}
	return strings.TrimSpace(afterWhitespace[len(thinkOpenTag):]), true
}

func stripThinkAnswerSeparator(text string) string {
	return strings.TrimLeft(text, "\r\n\t ")
}

func appendReasoningContentField(item map[string]any, reasoning string) bool {
	trimmed := strings.TrimSpace(reasoning)
	if trimmed == "" {
		return false
	}
	item["reasoning_content"] = trimmed
	return true
}

func AppendReasoningContent(message map[string]any, reasoning string) bool {
	trimmed := strings.TrimSpace(reasoning)
	if trimmed == "" {
		return false
	}
	if existing := strings.TrimSpace(stringValue(message["reasoning_content"])); existing != "" {
		message["reasoning_content"] = existing + "\n\n" + trimmed
	} else {
		message["reasoning_content"] = trimmed
	}
	return true
}
