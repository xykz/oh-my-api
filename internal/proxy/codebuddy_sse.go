package proxy

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"
)

// CodeBuddySSEDelta mirrors the delta field in CodeBuddy SSE chunks.
type CodeBuddySSEDelta struct {
	Content   string                 `json:"content"`
	ToolCalls []CodeBuddySSEToolCall `json:"tool_calls"`
}

// CodeBuddySSEToolCall mirrors a tool call delta in CodeBuddy SSE.
type CodeBuddySSEToolCall struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type CodeBuddySSEChunk struct {
	Choices []struct {
		Delta        CodeBuddySSEDelta `json:"delta"`
		FinishReason *string           `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

func parseCodeBuddySSELine(line string) (*CodeBuddySSEChunk, bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || !strings.HasPrefix(trimmed, "data:") {
		return nil, false
	}
	payload := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
	if payload == "[DONE]" {
		return nil, true
	}
	var chunk CodeBuddySSEChunk
	if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
		return nil, false
	}
	return &chunk, false
}

func ScanCodeBuddySSE(reader io.Reader, onChunk func(*CodeBuddySSEChunk) error, onDone func() error) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		chunk, done := parseCodeBuddySSELine(scanner.Text())
		if done {
			if onDone != nil {
				return onDone()
			}
			return nil
		}
		if chunk != nil && onChunk != nil {
			if err := onChunk(chunk); err != nil {
				return err
			}
		}
	}
	return scanner.Err()
}
