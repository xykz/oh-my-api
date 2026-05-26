package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/rizxfrog/oh-my-api/internal/api/model"
	"github.com/rizxfrog/oh-my-api/internal/api/response"
	"github.com/rizxfrog/oh-my-api/internal/api/service"
	"github.com/rizxfrog/oh-my-api/internal/proxy"
)

func (s *Server) HandleAnthropicMessages(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		response.WriteMethodNotAllowed(writer, http.MethodPost)
		return
	}

	body, err := io.ReadAll(io.LimitReader(request.Body, 2<<20))
	if err != nil {
		response.WriteAnthropicError(writer, http.StatusBadRequest, "read body failed")
		return
	}

	var anthropicReq proxy.AnthropicMessagesRequest
	if err := json.Unmarshal(body, &anthropicReq); err != nil {
		response.WriteAnthropicError(writer, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	if anthropicReq.Model == "" {
		response.WriteAnthropicError(writer, http.StatusBadRequest, "model is required")
		return
	}
	if len(anthropicReq.Messages) == 0 {
		response.WriteAnthropicError(writer, http.StatusBadRequest, "messages must not be empty")
		return
	}
	if anthropicReq.MaxTokens == 0 {
		anthropicReq.MaxTokens = 4096
	}

	sessionID := service.RequestSessionID(request, "")
	canonicalRequest, err := proxy.CanonicalizeAnthropicRequest(anthropicReq, sessionID)
	if err != nil {
		response.WriteAnthropicError(writer, http.StatusBadRequest, err.Error())
		return
	}
	service.AttachCanonicalRequestMetadata(&canonicalRequest, request.Header)

	if err := proxy.ValidateVisionLimits(canonicalRequest); err != nil {
		response.WriteAnthropicInvalidImage(writer, err.Error())
		return
	}
	var visionStore model.SettingsStore
	if s.DB != nil {
		visionStore = s.DB
	}
	if _, err := service.EvaluateVisionGate(request.Context(), visionStore, canonicalRequest); err != nil {
		if service.IsVisionNotImplemented(err) {
			response.WriteAnthropicVisionNotImplemented(writer)
			return
		}
		response.WriteAnthropicError(writer, http.StatusInternalServerError, err.Error())
		return
	}

	policyResult, err := service.EvaluateCanonicalRequest(request.Context(), s.DB, canonicalRequest)
	if err != nil {
		response.WriteAnthropicError(writer, http.StatusInternalServerError, "policy: "+err.Error())
		return
	}
	sessionCanonicalRequest, err := s.Deps.Sessions.BuildCanonicalRequest(request.Context(), sessionID, policyResult.PostPolicyRequest)
	if err != nil {
		response.WriteAnthropicError(writer, http.StatusInternalServerError, "sessions: "+err.Error())
		return
	}
	projectedRequest, projectedMessages, err := proxy.ProjectCanonicalToOpenAIRequest(sessionCanonicalRequest)
	if err != nil {
		response.WriteAnthropicError(writer, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateChatRequest(&projectedRequest); err != nil {
		response.WriteAnthropicError(writer, http.StatusBadRequest, err.Error())
		return
	}

	messages := projectedMessages

	resolvedModelKey, err := s.Deps.Models.ResolveChatModel(request.Context(), projectedRequest.Model)
	if err != nil {
		response.WriteAnthropicError(writer, http.StatusBadRequest, "resolve model: "+err.Error())
		return
	}
	if s.accountRoutingEnabled() {
		s.handleAccountRoutedAnthropicMessages(
			writer,
			request,
			anthropicReq,
			projectedRequest,
			sessionCanonicalRequest,
			canonicalRequest,
			policyResult.PostPolicyRequest,
			sessionID,
			messages,
			resolvedModelKey,
		)
		return
	}

	snapshot, err := s.Deps.Credentials.Current(request.Context())
	if err != nil {
		response.WriteAnthropicError(writer, http.StatusInternalServerError, "credentials: "+err.Error())
		return
	}

	remoteRequest, err := s.Deps.Builder.BuildCanonical(sessionCanonicalRequest, resolvedModelKey)
	if err != nil {
		response.WriteAnthropicError(writer, http.StatusInternalServerError, "build request: "+err.Error())
		return
	}

	stream, err := s.Deps.Transport.StreamChat(request.Context(), remoteRequest, snapshot)
	if err != nil {
		response.WriteAnthropicError(writer, http.StatusBadGateway, "upstream: "+err.Error())
		return
	}
	defer stream.Close()

	responseID := "msg_" + remoteRequest.RequestID
	traceID := proxy.NewUUID()

	if !anthropicReq.Stream {
		s.nonStreamAnthropicResponse(
			request.Context(),
			writer,
			stream,
			responseID,
			projectedRequest,
			remoteRequest,
			sessionID,
			messages,
			canonicalRequest,
			policyResult.PostPolicyRequest,
			sessionCanonicalRequest,
			traceID,
		)
	} else {
		s.streamAnthropicResponse(
			writer,
			request,
			stream,
			responseID,
			projectedRequest,
			remoteRequest,
			sessionID,
			messages,
			canonicalRequest,
			policyResult.PostPolicyRequest,
			sessionCanonicalRequest,
			traceID,
		)
	}
}

func (s *Server) handleAccountRoutedAnthropicMessages(
	writer http.ResponseWriter,
	request *http.Request,
	anthropicReq proxy.AnthropicMessagesRequest,
	projectedRequest proxy.OpenAIChatRequest,
	sessionCanonicalRequest proxy.CanonicalRequest,
	prePolicyRequest proxy.CanonicalRequest,
	postPolicyRequest proxy.CanonicalRequest,
	sessionID string,
	messages []proxy.Message,
	modelKey string,
) {
	account, adapter, err := s.selectAccountAndAdapter(request.Context(), modelKey)
	if err != nil {
		response.WriteAnthropicMappedError(writer, err)
		return
	}
	attachAccountRoutingMetadata(&prePolicyRequest, account)
	attachAccountRoutingMetadata(&postPolicyRequest, account)
	attachAccountRoutingMetadata(&sessionCanonicalRequest, account)
	imageURLs, err := s.uploadImagesWithAdapter(request.Context(), adapter, account, sessionCanonicalRequest)
	if err != nil {
		response.WriteAnthropicMappedError(writer, err)
		return
	}
	if len(imageURLs) > 0 {
		sessionCanonicalRequest.Metadata["image_urls"] = imageURLs
		sessionCanonicalRequest.Metadata["is_vl"] = true
	}

	remoteRequest, err := adapter.BuildChatRequest(request.Context(), sessionCanonicalRequest, modelKey, account)
	if err != nil {
		response.WriteAnthropicMappedError(writer, fmt.Errorf("build request: %w", err))
		return
	}
	stream, err := adapter.StreamChat(request.Context(), remoteRequest, account)
	if err != nil {
		response.WriteAnthropicMappedError(writer, fmt.Errorf("upstream: %w", err))
		return
	}
	defer stream.Close()

	responseID := "msg_" + remoteRequest.RequestID
	traceID := proxy.NewUUID()
	if !anthropicReq.Stream {
		s.nonStreamAnthropicResponse(
			request.Context(),
			writer,
			stream,
			responseID,
			projectedRequest,
			remoteRequest,
			sessionID,
			messages,
			prePolicyRequest,
			postPolicyRequest,
			sessionCanonicalRequest,
			traceID,
		)
		return
	}
	s.streamAnthropicResponse(
		writer,
		request,
		stream,
		responseID,
		projectedRequest,
		remoteRequest,
		sessionID,
		messages,
		prePolicyRequest,
		postPolicyRequest,
		sessionCanonicalRequest,
		traceID,
	)
}

func (s *Server) nonStreamAnthropicResponse(
	ctx context.Context,
	writer http.ResponseWriter,
	stream io.Reader,
	responseID string,
	request proxy.OpenAIChatRequest,
	remoteRequest proxy.RemoteChatRequest,
	sessionID string,
	messages []proxy.Message,
	prePolicyRequest proxy.CanonicalRequest,
	postPolicyRequest proxy.CanonicalRequest,
	sessionCanonicalRequest proxy.CanonicalRequest,
	traceID string,
) {
	var contentBuilder strings.Builder
	var reasoningBuilder strings.Builder
	var rawSSELines []string
	var inputTokens, outputTokens int
	var toolCalls []proxy.ToolCall
	err := proxy.ScanSSEWithLines(stream, func(line string) error {
		rawSSELines = append(rawSSELines, line)
		return nil
	}, func(event proxy.SSEEvent) error {
		if event.Usage != nil {
			inputTokens = event.Usage.PromptTokens
			outputTokens = event.Usage.CompletionTokens
		}
		contentBuilder.WriteString(event.Content)
		reasoningBuilder.WriteString(event.ReasoningContent)
		toolCalls = append(toolCalls, event.ToolCalls...)
		return nil
	})
	if err != nil {
		response.WriteAnthropicError(writer, http.StatusBadGateway, "upstream: "+err.Error())
		return
	}

	blocks := []proxy.ContentBlock{}

	reasoningText := reasoningBuilder.String()
	if reasoningText != "" {
		blocks = append(blocks, proxy.ContentBlock{
			Type:     "thinking",
			Thinking: reasoningText,
		})
	}

	contentText := contentBuilder.String()
	if contentText != "" {
		blocks = append(blocks, proxy.ContentBlock{
			Type: "text",
			Text: contentText,
		})
	}

	mergedToolCalls := mergeToolCallDeltas(toolCalls)
	for _, tc := range mergedToolCalls {
		blocks = append(blocks, proxy.ContentBlock{
			Type:  "tool_use",
			ID:    tc.ID,
			Name:  tc.Function.Name,
			Input: json.RawMessage(tc.Function.Arguments),
		})
	}

	stopReason := "end_turn"
	if len(mergedToolCalls) > 0 {
		stopReason = "tool_use"
	}

	if len(blocks) == 0 {
		blocks = append(blocks, proxy.ContentBlock{
			Type: "text",
			Text: contentText,
		})
	}

	promptTokens := inputTokens
	completionTokens := outputTokens
	if promptTokens <= 0 && completionTokens <= 0 {
		promptTokens = len(contentText)/4 + len(reasoningText)/4
		completionTokens = len(contentText)/4 + len(reasoningText)/4
	}
	totalTokens := promptTokens + completionTokens

	usage := proxy.Usage{
		InputTokens:  promptTokens,
		OutputTokens: completionTokens,
	}

	resp := proxy.AnthropicMessagesResponse{
		ID:         responseID,
		Type:       "message",
		Role:       "assistant",
		Content:    blocks,
		Model:      request.Model,
		StopReason: stopReason,
		Usage:      usage,
	}
	assistantContent := contentText
	if reasoningText != "" {
		assistantContent = "[thinking]" + reasoningText + "[/thinking]" + assistantContent
	}
	assistant := proxy.Message{
		Role:    "assistant",
		Content: assistantContent,
	}
	if err := s.Deps.Sessions.SaveCanonicalResponse(ctx, sessionID, sessionCanonicalRequest, assistant); err == nil {
		service.PersistCanonicalExecutionRecord(
			ctx,
			s.DB,
			s.Deps.Now(),
			s.StoreExecutionLogs,
			traceID,
			prePolicyRequest.Protocol,
			"/v1/messages",
			prePolicyRequest,
			postPolicyRequest,
			sessionCanonicalRequest,
			request,
			messages,
			assistant,
			remoteRequest,
			rawSSELines,
			promptTokens,
			completionTokens,
			totalTokens,
		)
		if s.TokenStats != nil {
			_ = s.TokenStats.AddTokens(ctx, totalTokens)
		}
	}

	response.WriteJSON(writer, http.StatusOK, resp)
}

func (s *Server) streamAnthropicResponse(
	writer http.ResponseWriter,
	request *http.Request,
	stream io.Reader,
	responseID string,
	chatRequest proxy.OpenAIChatRequest,
	remoteRequest proxy.RemoteChatRequest,
	sessionID string,
	messages []proxy.Message,
	prePolicyRequest proxy.CanonicalRequest,
	postPolicyRequest proxy.CanonicalRequest,
	sessionCanonicalRequest proxy.CanonicalRequest,
	traceID string,
) {
	flusher, ok := writer.(http.Flusher)
	if !ok {
		response.WriteAnthropicError(writer, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.Header().Set("Connection", "keep-alive")

	startUsage := proxy.Usage{}
	writeAnthropicSSE(writer, proxy.AnthropicStreamEvent{
		Type: "message_start",
		Message: &proxy.StreamMessage{
			ID:    responseID,
			Type:  "message",
			Role:  "assistant",
			Model: chatRequest.Model,
			Usage: startUsage,
		},
	})
	flusher.Flush()

	blockIndex := -1
	var blockStarted bool
	var blockType string
	var hasToolUse bool
	var contentBuilder strings.Builder
	var reasoningBuilder strings.Builder
	var rawSSELines []string
	var inputTokens, outputTokens int

	closeBlock := func() {
		if !blockStarted {
			return
		}
		writeAnthropicSSE(writer, proxy.AnthropicStreamEvent{
			Type:  "content_block_stop",
			Index: intPtr(blockIndex),
		})
		flusher.Flush()
		blockStarted = false
		blockType = ""
	}

	err := proxy.ScanSSEWithLines(stream, func(line string) error {
		rawSSELines = append(rawSSELines, line)
		return nil
	}, func(event proxy.SSEEvent) error {
		if event.Usage != nil {
			inputTokens = event.Usage.PromptTokens
			outputTokens = event.Usage.CompletionTokens
		}
		if event.Done {
			return nil
		}

		if event.ReasoningContent != "" {
			if blockStarted && blockType != "thinking" {
				closeBlock()
			}
			if !blockStarted {
				blockIndex++
				writeAnthropicSSE(writer, proxy.AnthropicStreamEvent{
					Type:  "content_block_start",
					Index: intPtr(blockIndex),
					ContentBlock: &proxy.ContentBlock{
						Type:     "thinking",
						Thinking: "",
					},
				})
				blockStarted = true
				blockType = "thinking"
			}
			writeAnthropicSSE(writer, proxy.AnthropicStreamEvent{
				Type:  "content_block_delta",
				Index: intPtr(blockIndex),
				Delta: &proxy.StreamDelta{
					Type:     "thinking_delta",
					Thinking: event.ReasoningContent,
				},
			})
			flusher.Flush()
			reasoningBuilder.WriteString(event.ReasoningContent)
			return nil
		}

		if event.Content != "" {
			if blockStarted && blockType != "text" {
				closeBlock()
			}
			if !blockStarted {
				blockIndex++
				writeAnthropicSSE(writer, proxy.AnthropicStreamEvent{
					Type:  "content_block_start",
					Index: intPtr(blockIndex),
					ContentBlock: &proxy.ContentBlock{
						Type: "text",
						Text: "",
					},
				})
				blockStarted = true
				blockType = "text"
			}
			writeAnthropicSSE(writer, proxy.AnthropicStreamEvent{
				Type:  "content_block_delta",
				Index: intPtr(blockIndex),
				Delta: &proxy.StreamDelta{
					Type: "text_delta",
					Text: event.Content,
				},
			})
			flusher.Flush()
			contentBuilder.WriteString(event.Content)
			return nil
		}

		for _, tc := range event.ToolCalls {
			isNew := tc.ID != "" || tc.Function.Name != ""
			if isNew {
				closeBlock()
				blockIndex++
				writeAnthropicSSE(writer, proxy.AnthropicStreamEvent{
					Type:  "content_block_start",
					Index: intPtr(blockIndex),
					ContentBlock: &proxy.ContentBlock{
						Type: "tool_use",
						ID:   tc.ID,
						Name: tc.Function.Name,
					},
				})
				blockStarted = true
				blockType = "tool_use"
				hasToolUse = true
			}
			if tc.Function.Arguments != "" {
				writeAnthropicSSE(writer, proxy.AnthropicStreamEvent{
					Type:  "content_block_delta",
					Index: intPtr(blockIndex),
					Delta: &proxy.StreamDelta{
						Type:        "input_json_delta",
						PartialJSON: tc.Function.Arguments,
					},
				})
				flusher.Flush()
			}
		}
		return nil
	})

	if err != nil {
		closeBlock()
		var buf bytes.Buffer
		_ = json.NewEncoder(&buf).Encode(map[string]string{
			"type":    "error",
			"message": err.Error(),
		})
		_, _ = fmt.Fprintf(writer, "event: error\ndata: %s\n\n", buf.String())
		flusher.Flush()
		return
	}

	closeBlock()

	totalText := contentBuilder.String()
	totalReasoning := reasoningBuilder.String()

	promptTokens := inputTokens
	completionTokens := outputTokens
	if completionTokens <= 0 {
		completionTokens = (len(totalText) + len(totalReasoning)) / 4
		if completionTokens == 0 {
			completionTokens = 1
		}
	}
	if promptTokens <= 0 {
		promptTokens = completionTokens * 2
	}
	totalTokens := promptTokens + completionTokens

	stopReason := "end_turn"
	if hasToolUse {
		stopReason = "tool_use"
	}

	writeAnthropicSSE(writer, proxy.AnthropicStreamEvent{
		Type: "message_delta",
		Delta: &proxy.StreamDelta{
			StopReason: stopReason,
		},
		Usage: &proxy.Usage{
			OutputTokens: completionTokens,
			InputTokens:  promptTokens,
		},
	})
	flusher.Flush()

	writeAnthropicSSE(writer, proxy.AnthropicStreamEvent{
		Type: "message_stop",
	})
	flusher.Flush()

	assistantContent := totalText
	if totalReasoning != "" {
		assistantContent = "[thinking]" + totalReasoning + "[/thinking]" + assistantContent
	}
	assistant := proxy.Message{
		Role:    "assistant",
		Content: assistantContent,
	}
	if err := s.Deps.Sessions.SaveCanonicalResponse(request.Context(), sessionID, sessionCanonicalRequest, assistant); err == nil {
		service.PersistCanonicalExecutionRecord(
			request.Context(),
			s.DB,
			s.Deps.Now(),
			s.StoreExecutionLogs,
			traceID,
			prePolicyRequest.Protocol,
			"/v1/messages",
			prePolicyRequest,
			postPolicyRequest,
			sessionCanonicalRequest,
			chatRequest,
			messages,
			assistant,
			remoteRequest,
			rawSSELines,
			promptTokens,
			completionTokens,
			totalTokens,
		)
		if s.TokenStats != nil {
			_ = s.TokenStats.AddTokens(request.Context(), totalTokens)
		}
	}
}

func writeAnthropicSSE(writer http.ResponseWriter, event proxy.AnthropicStreamEvent) {
	var buf bytes.Buffer
	_ = json.NewEncoder(&buf).Encode(event)
	data := strings.TrimSpace(buf.String())
	eventType := event.Type
	fmt.Fprintf(writer, "event: %s\ndata: %s\n\n", eventType, data)
}

func intPtr(i int) *int { return &i }

func mergeToolCallDeltas(deltas []proxy.ToolCall) []proxy.ToolCall {
	if len(deltas) == 0 {
		return nil
	}
	merged := make(map[int]*proxy.ToolCall)
	order := make([]int, 0, len(deltas))
	for _, tc := range deltas {
		idx := tc.Index
		if existing, ok := merged[idx]; ok {
			if tc.ID != "" {
				existing.ID = tc.ID
			}
			if tc.Function.Name != "" {
				existing.Function.Name = tc.Function.Name
			}
			if tc.Function.Arguments != "" {
				existing.Function.Arguments += tc.Function.Arguments
			}
		} else {
			merged[idx] = &proxy.ToolCall{
				Index: idx,
				ID:    tc.ID,
				Type:  tc.Type,
				Function: proxy.FunctionCall{
					Name:      tc.Function.Name,
					Arguments: tc.Function.Arguments,
				},
			}
			order = append(order, idx)
		}
	}
	result := make([]proxy.ToolCall, 0, len(merged))
	for _, idx := range order {
		result = append(result, *merged[idx])
	}
	return result
}
