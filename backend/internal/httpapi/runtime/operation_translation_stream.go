package runtime

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/coachpo/prism/backend/internal/gateway/provider/openai"
)

type openAIStreamTranslator interface {
	consumeEvent(string, map[string]any) ([][]byte, error)
	consumeDone() ([][]byte, error)
}

func proxyEventStreamAndCaptureCompletedResponseByOperation(operation RuntimeOperation, translationMode TranslationMode, requestedModelID string, ctx context.Context, dst io.Writer, src io.Reader, now func() time.Time, captureAuditBody bool) (runtimeResponseCapture, error) {
	if translationMode == "" || translationMode == TranslationModeNone {
		return proxyEventStreamAndCaptureCompletedResponse(operation, ctx, dst, src, now, captureAuditBody)
	}
	streamHooks, translator, err := newOpenAIStreamTranslator(translationMode, requestedModelID)
	if err != nil {
		return runtimeResponseCapture{}, err
	}
	reader := bufio.NewReader(src)
	capture := sseCompletedResponseCapture{streamHooks: streamHooks}
	var auditBuffer bytes.Buffer
	var currentEvent string
	currentDataLines := make([]string, 0, 4)

	captureResult := func(classification sseStreamClassification) runtimeResponseCapture {
		responseCapture := capture.runtimeResponseCapture(classification)
		if captureAuditBody {
			responseCapture.AuditBody = append([]byte(nil), auditBuffer.Bytes()...)
		}
		return responseCapture
	}
	flushEvent := func() (error, error) {
		if len(currentDataLines) == 0 {
			currentEvent = ""
			currentDataLines = currentDataLines[:0]
			return nil, nil
		}
		payloadBytes := []byte(strings.Join(currentDataLines, "\n"))
		eventName := currentEvent
		currentEvent = ""
		currentDataLines = currentDataLines[:0]
		var frames [][]byte
		if strings.TrimSpace(string(payloadBytes)) == "[DONE]" {
			frames, err = translator.consumeDone()
		} else {
			payload := map[string]any{}
			if unmarshalErr := json.Unmarshal(payloadBytes, &payload); unmarshalErr == nil {
				frames, err = translator.consumeEvent(eventName, payload)
			}
		}
		if err != nil {
			return nil, err
		}
		for _, frame := range frames {
			if len(frame) == 0 {
				continue
			}
			if _, writeErr := dst.Write(frame); writeErr != nil {
				return writeErr, nil
			}
		}
		return nil, nil
	}
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			observedAt := now()
			capture.consumeLine(line, observedAt)
			if captureAuditBody {
				auditBuffer.Write(line)
			}
			trimmed := strings.TrimRight(string(line), "\r\n")
			switch {
			case trimmed == "":
				if writeErr, translateErr := flushEvent(); writeErr != nil {
					return captureResult(classifySSEStreamOutcome(ctx, capture.terminalSignal, nil, writeErr)), writeErr
				} else if translateErr != nil {
					return captureResult(classifySSEStreamOutcome(ctx, capture.terminalSignal, translateErr, nil)), translateErr
				}
			case strings.HasPrefix(trimmed, "event:"):
				currentEvent = trimSSEFieldValue(strings.TrimPrefix(trimmed, "event:"))
			case strings.HasPrefix(trimmed, "data:"):
				currentDataLines = append(currentDataLines, trimSSEFieldValue(strings.TrimPrefix(trimmed, "data:")))
			}
		}
		if readErr == nil {
			continue
		}
		capture.finishEvent(now())
		if writeErr, translateErr := flushEvent(); writeErr != nil {
			return captureResult(classifySSEStreamOutcome(ctx, capture.terminalSignal, nil, writeErr)), writeErr
		} else if translateErr != nil {
			return captureResult(classifySSEStreamOutcome(ctx, capture.terminalSignal, translateErr, nil)), translateErr
		}
		if errors.Is(readErr, io.EOF) {
			return captureResult(classifySSEStreamOutcome(ctx, capture.terminalSignal, nil, nil)), nil
		}
		return captureResult(classifySSEStreamOutcome(ctx, capture.terminalSignal, readErr, nil)), readErr
	}
}

func newOpenAIStreamTranslator(mode TranslationMode, requestedModelID string) (operationStreamHooks, openAIStreamTranslator, error) {
	hooks, ok := translatedOpenAIStreamHooksForMode(mode)
	if !ok {
		return operationStreamHooks{}, nil, unsupportedTranslationModeError(mode)
	}
	translator, err := openai.NewStreamTranslator(providerTranslationMode(mode), requestedModelID)
	if err != nil {
		return operationStreamHooks{}, nil, err
	}
	return hooks, providerOpenAIStreamTranslator{translator: translator}, nil
}

type providerOpenAIStreamTranslator struct {
	translator openai.StreamTranslator
}

func (translator providerOpenAIStreamTranslator) consumeEvent(event string, payload map[string]any) ([][]byte, error) {
	frames, err := translator.translator.ConsumeEvent(event, payload)
	return frames, domainErrorFromOpenAIStreamAdapterError(err)
}

func (translator providerOpenAIStreamTranslator) consumeDone() ([][]byte, error) {
	frames, err := translator.translator.ConsumeDone()
	return frames, domainErrorFromOpenAIStreamAdapterError(err)
}

func domainErrorFromOpenAIStreamAdapterError(err error) error {
	if err == nil {
		return nil
	}
	if domainErr := domainErrorFromProviderAdapterError(err); domainErr != nil {
		return domainErr
	}
	return err
}

func translatedOpenAIStreamHooksForMode(mode TranslationMode) (operationStreamHooks, bool) {
	switch mode {
	case TranslationModeOpenAIResponsesToChatCompletions:
		return operationStreamHooksByCollectionID[openAIUpstreamOperationChatCompletions], true
	case TranslationModeOpenAIChatCompletionsToResponses:
		return operationStreamHooksByCollectionID[openAIUpstreamOperationResponses], true
	default:
		return operationStreamHooks{}, false
	}
}
