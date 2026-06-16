package openai

import "strings"

type TextItemState struct {
	OutputIndex *int
	ItemID      string
	Text        strings.Builder
	Added       bool
	Done        bool
}

type ReasoningItemState struct {
	OutputIndex *int
	ItemID      string
	Text        strings.Builder
	Added       bool
	Done        bool
}

type InlineThinkMode string

const (
	InlineThinkDetecting InlineThinkMode = "detecting"
	InlineThinkReasoning InlineThinkMode = "reasoning"
	InlineThinkText      InlineThinkMode = "text"
)

type InlineThinkState struct {
	Mode   InlineThinkMode
	Buffer strings.Builder
}

func NewInlineThinkState() InlineThinkState {
	return InlineThinkState{Mode: InlineThinkDetecting}
}

type InlineThinkDecision string

const (
	InlineThinkNeedMore          InlineThinkDecision = "need_more"
	InlineThinkReasoningDecision InlineThinkDecision = "reasoning"
	InlineThinkTextDecision      InlineThinkDecision = "text"
)

func (state *InlineThinkState) Push(delta string) (InlineThinkDecision, string, string) {
	if state.Mode == "" {
		state.Mode = InlineThinkDetecting
	}
	state.Buffer.WriteString(delta)
	buffered := state.Buffer.String()
	switch state.Mode {
	case InlineThinkText:
		state.Buffer.Reset()
		return InlineThinkTextDecision, "", buffered
	case InlineThinkReasoning:
		if reasoning, answer, ok := splitLeadingThinkBlock(buffered); ok {
			state.Mode = InlineThinkText
			state.Buffer.Reset()
			return InlineThinkReasoningDecision, reasoning, answer
		}
		return InlineThinkNeedMore, "", ""
	default:
		decision := leadingThinkPrefixDecision(buffered)
		if decision == InlineThinkReasoningDecision {
			state.Mode = InlineThinkReasoning
			if reasoning, answer, ok := splitLeadingThinkBlock(buffered); ok {
				state.Mode = InlineThinkText
				state.Buffer.Reset()
				return InlineThinkReasoningDecision, reasoning, answer
			}
			return InlineThinkNeedMore, "", ""
		}
		if decision == InlineThinkTextDecision {
			state.Mode = InlineThinkText
			state.Buffer.Reset()
			return InlineThinkTextDecision, "", buffered
		}
		return InlineThinkNeedMore, "", ""
	}
}

func leadingThinkPrefixDecision(text string) InlineThinkDecision {
	leadingWhitespaceLength := len(text) - len(strings.TrimLeft(text, "\r\n\t "))
	afterWhitespace := text[leadingWhitespaceLength:]
	if strings.HasPrefix(afterWhitespace, thinkOpenTag) {
		return InlineThinkReasoningDecision
	}
	if strings.HasPrefix(thinkOpenTag, afterWhitespace) {
		return InlineThinkNeedMore
	}
	return InlineThinkTextDecision
}

func (state *InlineThinkState) FlushAtBoundary() (string, string) {
	buffered := state.Buffer.String()
	state.Buffer.Reset()
	if state.Mode == InlineThinkReasoning {
		state.Mode = InlineThinkText
		if reasoning, answer, ok := splitLeadingThinkBlock(buffered); ok {
			return reasoning, answer
		}
		if stripped, ok := stripLeadingThinkOpenTag(buffered); ok {
			return stripped, ""
		}
		return buffered, ""
	}
	state.Mode = InlineThinkText
	return "", buffered
}

type ToolCallState struct {
	OutputIndex      *int
	ItemID           string
	CallID           string
	Name             string
	Arguments        strings.Builder
	ReasoningContent strings.Builder
	Added            bool
	Done             bool
}

func (state *ToolCallState) ApplyDelta(callID string, name string, arguments string, reasoning string) {
	if strings.TrimSpace(callID) != "" {
		state.CallID = callID
	}
	if strings.TrimSpace(name) != "" {
		state.Name = name
	}
	state.Arguments.WriteString(arguments)
	state.ReasoningContent.WriteString(reasoning)
}

func (state *ToolCallState) CanonicalArguments() string {
	return canonicalToolArguments(state.Arguments.String())
}

type ChatToResponsesState struct {
	ResponseStarted bool
	Completed       bool
	ResponseID      string
	Model           string
	CreatedAt       *int
	NextOutputIndex int
	Text            TextItemState
	Reasoning       ReasoningItemState
	InlineThink     InlineThinkState
	Tools           map[int]*ToolCallState
	OutputItems     []map[string]any
	LatestUsage     map[string]any
	FinishReason    string
	ToolContext     *ToolContext
}

func NewChatToResponsesState(context *ToolContext) *ChatToResponsesState {
	if context == nil {
		context = NewToolContext()
	}
	return &ChatToResponsesState{ResponseID: "resp_prism", InlineThink: NewInlineThinkState(), Tools: map[int]*ToolCallState{}, ToolContext: context}
}

func (state *ChatToResponsesState) AllocateOutputIndex() int {
	index := state.NextOutputIndex
	state.NextOutputIndex++
	return index
}

func (state *ChatToResponsesState) HasSubstantiveOutput() bool {
	if state == nil {
		return false
	}
	if strings.TrimSpace(state.Text.Text.String()) != "" || strings.TrimSpace(state.Reasoning.Text.String()) != "" || strings.TrimSpace(state.InlineThink.Buffer.String()) != "" || len(state.OutputItems) > 0 {
		return true
	}
	for _, tool := range state.Tools {
		if tool.Added || strings.TrimSpace(tool.CallID) != "" || strings.TrimSpace(tool.Name) != "" || strings.TrimSpace(tool.Arguments.String()) != "" || strings.TrimSpace(tool.ReasoningContent.String()) != "" {
			return true
		}
	}
	return false
}
