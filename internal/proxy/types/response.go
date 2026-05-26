package types

import "encoding/json"

// ── Request types ─────────────────────────────────────────────────

type OpenAIResponseRequest struct {
	Model              string               `json:"model"`
	Input              json.RawMessage      `json:"input"`
	Instructions       string               `json:"instructions"`
	MaxOutputTokens    int                  `json:"max_output_tokens"`
	Temperature        float64              `json:"temperature"`
	TopP               float64              `json:"top_p"`
	Stream             bool                 `json:"stream"`
	Store              bool                 `json:"store"`
	PreviousResponseID string               `json:"previous_response_id"`
	Tools              []OpenAIResponseTool `json:"tools"`
	ToolChoice         any                  `json:"tool_choice"`
	Conversation       string               `json:"conversation"`
	Metadata           map[string]string    `json:"metadata"`
}

type ResponseInputItem struct {
	Type      string                     `json:"type"`
	Role      string                     `json:"role,omitempty"`
	Content   json.RawMessage            `json:"content,omitempty"`
	Name      string                     `json:"name,omitempty"`
	Arguments string                     `json:"arguments,omitempty"`
	CallID    string                     `json:"call_id,omitempty"`
	Output    string                     `json:"output,omitempty"`
	Summary   []ResponseInputContentPart `json:"summary,omitempty"`
}

type ResponseInputContentPart struct {
	Type     string        `json:"type"`
	Text     string        `json:"text,omitempty"`
	ImageURL *ImageURLPart `json:"image_url,omitempty"`
}

type ImageURLPart struct {
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

type OpenAIResponseTool struct {
	Type        string `json:"type"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	Parameters  any    `json:"parameters,omitempty"`
}

// ── Response types ────────────────────────────────────────────────

type OpenAIResponse struct {
	ID                 string                 `json:"id"`
	Object             string                 `json:"object"`
	CreatedAt          int64                  `json:"created_at"`
	Status             string                 `json:"status"`
	StatusDetails      *ResponseStatusDetails `json:"status_details,omitempty"`
	Model              string                 `json:"model"`
	Output             []ResponseOutputItem   `json:"output"`
	Usage              *OpenAIResponseUsage   `json:"usage,omitempty"`
	Instructions       string                 `json:"instructions,omitempty"`
	MaxOutputTokens    int                    `json:"max_output_tokens"`
	Temperature        float64                `json:"temperature"`
	TopP               float64                `json:"top_p"`
	ParallelToolCalls  bool                   `json:"parallel_tool_calls"`
	PreviousResponseID string                 `json:"previous_response_id,omitempty"`
}

type ResponseStatusDetails struct {
	Type   string `json:"type"`
	Reason string `json:"reason"`
}

type ResponseOutputItem struct {
	ID        string                  `json:"id"`
	Type      string                  `json:"type"`
	Status    string                  `json:"status"`
	Role      string                  `json:"role,omitempty"`
	Content   []ResponseOutputContent `json:"content,omitempty"`
	Name      string                  `json:"name,omitempty"`
	Arguments string                  `json:"arguments,omitempty"`
	CallID    string                  `json:"call_id,omitempty"`
	Summary   []ResponseOutputContent `json:"summary,omitempty"`
}

type ResponseOutputContent struct {
	Type        string               `json:"type"`
	Text        string               `json:"text"`
	Annotations []ResponseAnnotation `json:"annotations,omitempty"`
}

type ResponseAnnotation struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	URL  string `json:"url,omitempty"`
}

type OpenAIResponseUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	TotalTokens  int `json:"total_tokens"`
}

// ── Streaming types ───────────────────────────────────────────────

type ResponseStreamEvent struct {
	Type         string              `json:"type"`
	Response     *OpenAIResponse     `json:"response,omitempty"`
	ItemID       string              `json:"item_id,omitempty"`
	OutputIndex  int                 `json:"output_index,omitempty"`
	ContentIndex int                 `json:"content_index,omitempty"`
	Delta        string              `json:"delta,omitempty"`
	Item         *ResponseOutputItem `json:"item,omitempty"`
	Part         *ResponseOutputItem `json:"part,omitempty"`
}
