package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/rizxfrog/oh-my-api/internal/api/response"
	"github.com/rizxfrog/oh-my-api/internal/proxy"
)

func (s *Server) HandleCodeBuddyChat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.WriteMethodNotAllowed(w, http.MethodPost)
		return
	}
	if s.CodeBuddyClient == nil {
		response.WriteOpenAIError(w, http.StatusServiceUnavailable, "codebuddy not configured")
		return
	}

	account, apiKey, err := s.selectCodeBuddyAccount(r.Context())
	if err != nil {
		response.WriteOpenAIError(w, http.StatusUnauthorized, "no codebuddy account: "+err.Error())
		return
	}
	_ = account // reserved for future metadata use

	body := http.MaxBytesReader(w, r.Body, 2<<20)
	defer body.Close()
	var chatReq proxy.OpenAIChatRequest
	if err := json.NewDecoder(body).Decode(&chatReq); err != nil {
		response.WriteOpenAIError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	stream, err := s.CodeBuddyClient.SendChat(r.Context(), apiKey, chatReq)
	if err != nil {
		response.WriteOpenAIError(w, http.StatusBadGateway, "upstream: "+err.Error())
		return
	}
	defer stream.Close()

	if chatReq.Stream {
		s.codebuddyStreamResponse(w, r, stream, chatReq.Model)
	} else {
		s.codebuddyNonStreamResponse(w, r.Context(), stream, chatReq.Model)
	}
}

func (s *Server) selectCodeBuddyAccount(ctx context.Context) (proxy.AccountSnapshot, string, error) {
	if s.Deps.Accounts == nil {
		return proxy.AccountSnapshot{}, "", fmt.Errorf("account provider not configured")
	}
	accounts, err := s.Deps.Accounts.Accounts(ctx)
	if err != nil {
		return proxy.AccountSnapshot{}, "", err
	}
	enabled := make([]proxy.AccountSnapshot, 0)
	for _, a := range accounts {
		if a.Region == proxy.AccountRegionCodeBuddy && a.Enabled {
			enabled = append(enabled, a)
		}
	}
	if len(enabled) == 0 {
		return proxy.AccountSnapshot{}, "", fmt.Errorf("no enabled codebuddy accounts")
	}
	idx := atomic.AddUint64(&s.CodeBuddyRRIndex, 1) % uint64(len(enabled))
	account := enabled[idx]
	return account, account.AccessToken, nil
}

func (s *Server) codebuddyNonStreamResponse(w http.ResponseWriter, ctx context.Context, stream io.Reader, model string) {
	startTime := s.Deps.Now()
	var contentBuilder strings.Builder
	var rawLines []string
	var promptTokens, completionTokens int

	err := proxy.ScanCodeBuddySSE(stream, func(chunk *proxy.CodeBuddySSEChunk) error {
		rawLine, _ := json.Marshal(chunk)
		rawLines = append(rawLines, string(rawLine))
		if chunk.Usage != nil {
			promptTokens = chunk.Usage.PromptTokens
			completionTokens = chunk.Usage.CompletionTokens
		}
		for _, choice := range chunk.Choices {
			contentBuilder.WriteString(choice.Delta.Content)
		}
		return nil
	}, nil)
	if err != nil {
		response.WriteOpenAIError(w, http.StatusBadGateway, "upstream: "+err.Error())
		return
	}

	content := contentBuilder.String()
	totalTokens := promptTokens + completionTokens
	if totalTokens == 0 {
		totalTokens = len(content) / 2
		promptTokens = totalTokens / 2
		completionTokens = totalTokens - promptTokens
	}

	if s.TokenStats != nil {
		_ = s.TokenStats.AddTokens(ctx, totalTokens)
	}
	if s.RequestStats != nil {
		ttftMs := int(s.Deps.Now().Sub(startTime).Milliseconds())
		_ = s.RequestStats.RecordRequest(ctx, true, ttftMs)
	}

	_ = rawLines // reserved for future logging

	response.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"id":      "chatcmpl-" + proxy.NewHexID(),
		"object":  "chat.completion",
		"created": s.Deps.Now().Unix(),
		"model":   model,
		"choices": []map[string]interface{}{{
			"index": 0,
			"message": map[string]string{
				"role":    "assistant",
				"content": content,
			},
			"finish_reason": "stop",
		}},
		"usage": map[string]int{
			"prompt_tokens":     promptTokens,
			"completion_tokens": completionTokens,
			"total_tokens":      totalTokens,
		},
	})
}

func (s *Server) codebuddyStreamResponse(w http.ResponseWriter, r *http.Request, stream io.Reader, model string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		response.WriteOpenAIError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	startTime := s.Deps.Now()
	var firstTokenTime time.Time
	var totalContent strings.Builder
	var promptTokens, completionTokens int
	toolCallIndexMap := map[string]int{}

	responseID := "chatcmpl-" + proxy.NewHexID()

	err := proxy.ScanCodeBuddySSE(stream, func(chunk *proxy.CodeBuddySSEChunk) error {
		if chunk.Usage != nil {
			promptTokens = chunk.Usage.PromptTokens
			completionTokens = chunk.Usage.CompletionTokens
			return nil
		}
		for _, choice := range chunk.Choices {
			delta := choice.Delta
			converted := convertCodeBuddyDelta(delta, toolCallIndexMap)
			if delta.Content != "" {
				if firstTokenTime.IsZero() {
					firstTokenTime = s.Deps.Now()
				}
				totalContent.WriteString(delta.Content)
			}
			chunkResp := map[string]interface{}{
				"id":      responseID,
				"object":  "chat.completion.chunk",
				"created": s.Deps.Now().Unix(),
				"model":   model,
				"choices": []map[string]interface{}{{
					"index": 0,
					"delta": converted,
				}},
			}
			if choice.FinishReason != nil {
				chunkResp["choices"].([]map[string]interface{})[0]["finish_reason"] = *choice.FinishReason
			}
			data, _ := json.Marshal(chunkResp)
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
		return nil
	}, func() error {
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
		return nil
	})
	if err != nil {
		fmt.Fprintf(w, "data: {\"error\":{\"message\":%q}}\n\n", err.Error())
		flusher.Flush()
		return
	}

	totalTokens := promptTokens + completionTokens
	if totalTokens == 0 {
		totalTokens = totalContent.Len() / 2
	}
	if s.TokenStats != nil {
		_ = s.TokenStats.AddTokens(r.Context(), totalTokens)
	}
	if s.RequestStats != nil && !firstTokenTime.IsZero() {
		ttftMs := int(firstTokenTime.Sub(startTime).Milliseconds())
		_ = s.RequestStats.RecordRequest(r.Context(), true, ttftMs)
	}
}

func convertCodeBuddyDelta(delta proxy.CodeBuddySSEDelta, toolCallIndexMap map[string]int) map[string]interface{} {
	result := map[string]interface{}{}
	if delta.Content != "" {
		result["content"] = delta.Content
	}
	if len(delta.ToolCalls) > 0 {
		toolCalls := make([]map[string]interface{}, 0, len(delta.ToolCalls))
		for _, tc := range delta.ToolCalls {
			convertedID := tc.ID
			if strings.HasPrefix(convertedID, "tooluse_") {
				convertedID = "call_" + convertedID[len("tooluse_"):]
			}
			if _, exists := toolCallIndexMap[convertedID]; !exists {
				toolCallIndexMap[convertedID] = len(toolCallIndexMap)
			}
			tcMap := map[string]interface{}{
				"index": toolCallIndexMap[convertedID],
				"id":    convertedID,
				"type":  tc.Type,
			}
			if tc.Function.Name != "" || tc.Function.Arguments != "" {
				tcMap["function"] = map[string]string{
					"name":      tc.Function.Name,
					"arguments": tc.Function.Arguments,
				}
			}
			toolCalls = append(toolCalls, tcMap)
		}
		result["tool_calls"] = toolCalls
	}
	return result
}

func (s *Server) HandleCodeBuddyModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.WriteMethodNotAllowed(w, http.MethodGet)
		return
	}
	models := s.Deps.CodeBuddyConfig.Models
	data := make([]map[string]interface{}, 0, len(models))
	for _, m := range models {
		data = append(data, map[string]interface{}{
			"id":       m,
			"object":   "model",
			"created":  s.Deps.Now().Unix(),
			"owned_by": "codebuddy",
		})
	}
	response.WriteJSON(w, http.StatusOK, map[string]interface{}{
		"object": "list",
		"data":   data,
	})
}
