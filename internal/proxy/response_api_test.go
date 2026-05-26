package proxy

import (
	"encoding/json"
	"testing"
)

func TestCanonicalizeOpenAIResponseRequest_StringInput(t *testing.T) {
	input := `"Hello, world!"`
	raw := json.RawMessage(input)
	req := OpenAIResponseRequest{
		Model: "test-model",
		Input: raw,
	}
	canonical, err := CanonicalizeOpenAIResponseRequest(req, "sess-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if canonical.Protocol != CanonicalProtocolResponse {
		t.Errorf("expected protocol %q, got %q", CanonicalProtocolResponse, canonical.Protocol)
	}
	if canonical.SessionID != "sess-1" {
		t.Errorf("expected session %q, got %q", "sess-1", canonical.SessionID)
	}
	if len(canonical.Turns) == 0 {
		t.Fatal("expected at least one turn")
	}
}

func TestCanonicalizeOpenAIResponseRequest_ArrayInput(t *testing.T) {
	req := OpenAIResponseRequest{
		Model: "test-model",
		Input: json.RawMessage(`[{"type":"message","role":"user","content":"hello"}]`),
	}
	canonical, err := CanonicalizeOpenAIResponseRequest(req, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(canonical.Turns) != 1 {
		t.Fatalf("expected 1 turn, got %d", len(canonical.Turns))
	}
}

func TestCanonicalizeOpenAIResponseRequest_WithInstructions(t *testing.T) {
	req := OpenAIResponseRequest{
		Model:        "test-model",
		Instructions: "You are helpful.",
		Input:        json.RawMessage(`"hello"`),
	}
	canonical, err := CanonicalizeOpenAIResponseRequest(req, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(canonical.Turns) < 2 {
		t.Fatalf("expected at least 2 turns (instructions + input), got %d", len(canonical.Turns))
	}
	if canonical.Turns[0].Role != "system" {
		t.Errorf("expected first turn role 'system', got %q", canonical.Turns[0].Role)
	}
}

func TestCanonicalizeOpenAIResponseRequest_PreviousResponseID(t *testing.T) {
	req := OpenAIResponseRequest{
		Model:              "test-model",
		PreviousResponseID: "resp_123",
		Input:              json.RawMessage(`"hello"`),
	}
	canonical, err := CanonicalizeOpenAIResponseRequest(req, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if canonical.SessionID != "resp_123" {
		t.Errorf("expected sessionID 'resp_123', got %q", canonical.SessionID)
	}
}

func TestCanonicalizeOpenAIResponseRequest_BuiltinTools(t *testing.T) {
	req := OpenAIResponseRequest{
		Model: "test-model",
		Input: json.RawMessage(`"hello"`),
		Tools: []OpenAIResponseTool{
			{Type: "function", Name: "get_weather", Description: "get weather"},
			{Type: "web_search_preview"},
			{Type: "code_interpreter"},
		},
	}
	canonical, err := CanonicalizeOpenAIResponseRequest(req, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(canonical.Tools) != 1 {
		t.Fatalf("expected 1 function tool, got %d", len(canonical.Tools))
	}
	builtin, _ := canonical.Metadata["openai_builtin_tools"].([]string)
	if len(builtin) != 2 {
		t.Fatalf("expected 2 builtin tools in metadata, got %d", len(builtin))
	}
}

func TestCanonicalizeOpenAIResponseRequest_MessageWithImage(t *testing.T) {
	req := OpenAIResponseRequest{
		Model: "test-model",
		Input: json.RawMessage(`[{"type":"message","role":"user","content":[{"type":"input_text","text":"describe this"},{"type":"input_image","image_url":{"url":"data:image/png;base64,AAAA"}}]}]`),
	}
	canonical, err := CanonicalizeOpenAIResponseRequest(req, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(canonical.Turns) != 1 {
		t.Fatalf("expected 1 turn, got %d", len(canonical.Turns))
	}
	turn := canonical.Turns[0]
	if len(turn.Blocks) != 2 {
		t.Fatalf("expected 2 blocks (text + image), got %d", len(turn.Blocks))
	}
}

func TestCanonicalizeOpenAIResponseRequest_EmptyModel(t *testing.T) {
	_, err := CanonicalizeOpenAIResponseRequest(OpenAIResponseRequest{Input: json.RawMessage(`"hello"`)}, "")
	if err == nil {
		t.Fatal("expected error for empty model")
	}
}
