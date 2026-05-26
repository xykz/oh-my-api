package types

import (
	"bytes"
	"encoding/json"
	"fmt"
)

type AnthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
}

type AnthropicMessagesRequest struct {
	Model       string             `json:"model"`
	Messages    []AnthropicMessage `json:"messages"`
	System      json.RawMessage    `json:"system,omitempty"`
	Tools       []AnthropicTool    `json:"tools,omitempty"`
	ToolChoice  any                `json:"tool_choice,omitempty"`
	MaxTokens   int                `json:"max_tokens"`
	Temperature *float64           `json:"temperature,omitempty"`
	TopP        *float64           `json:"top_p,omitempty"`
	TopK        *int               `json:"top_k,omitempty"`
	Stream      bool               `json:"stream,omitempty"`
	Thinking    *ThinkingConfig    `json:"thinking,omitempty"`
	Metadata    json.RawMessage    `json:"metadata,omitempty"`
}

type ThinkingConfig struct {
	Type         string `json:"type"`
	BudgetTokens *int   `json:"budget_tokens,omitempty"`
}

type AnthropicMessage struct {
	Role    string         `json:"role"`
	Content []ContentBlock `json:"content"`
}

// UnmarshalJSON accepts both string and content-block-array forms for `content`,
// matching the Anthropic API spec where content can be a plain string or an array.
func (m *AnthropicMessage) UnmarshalJSON(data []byte) error {
	type messageAux struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	var aux messageAux
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	m.Role = aux.Role
	m.Content = nil

	raw := bytes.TrimSpace(aux.Content)
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return fmt.Errorf("content as string: %w", err)
		}
		m.Content = []ContentBlock{{Type: "text", Text: s}}
		return nil
	}
	if raw[0] == '[' {
		return json.Unmarshal(raw, &m.Content)
	}
	return fmt.Errorf("content must be string or array, got %s", string(raw[:1]))
}

type ContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
	IsError   *bool           `json:"is_error,omitempty"`
	Source    *ImageSource    `json:"source,omitempty"`
	Thinking  string          `json:"thinking,omitempty"`
	Signature string          `json:"signature,omitempty"`
}

type ImageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
}

type SystemBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type AnthropicMessagesResponse struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"`
	Role       string         `json:"role"`
	Content    []ContentBlock `json:"content"`
	Model      string         `json:"model"`
	StopReason string         `json:"stop_reason"`
	Usage      Usage          `json:"usage"`
}

type Usage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
}

type AnthropicStreamEvent struct {
	Type         string         `json:"type"`
	Index        *int           `json:"index,omitempty"`
	Message      *StreamMessage `json:"message,omitempty"`
	ContentBlock *ContentBlock  `json:"content_block,omitempty"`
	Delta        *StreamDelta   `json:"delta,omitempty"`
	Usage        *Usage         `json:"usage,omitempty"`
}

type StreamMessage struct {
	ID    string `json:"id"`
	Type  string `json:"type"`
	Role  string `json:"role"`
	Model string `json:"model"`
	Usage Usage  `json:"usage"`
}

type StreamDelta struct {
	Type         string `json:"type,omitempty"`
	Text         string `json:"text,omitempty"`
	PartialJSON  string `json:"partial_json,omitempty"`
	Thinking     string `json:"thinking,omitempty"`
	Signature    string `json:"signature,omitempty"`
	StopReason   string `json:"stop_reason,omitempty"`
	StopSequence string `json:"stop_sequence,omitempty"`
}
