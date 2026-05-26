package handler

import (
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

// ── Response API handler ────────────────────────────────────────

func (s *Server) HandleResponses(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		response.WriteMethodNotAllowed(writer, http.MethodPost)
		return
	}

	body := http.MaxBytesReader(writer, request.Body, 2<<20)
	defer body.Close()

	var respReq proxy.OpenAIResponseRequest
	if err := json.NewDecoder(body).Decode(&respReq); err != nil {
		response.WriteResponseError(writer, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	if respReq.Model == "" {
		response.WriteResponseError(writer, http.StatusBadRequest, "model is required")
		return
	}

	sessionID := service.RequestSessionID(request, respReq.PreviousResponseID)
	canonicalRequest, err := proxy.CanonicalizeOpenAIResponseRequest(respReq, sessionID)
	if err != nil {
		response.WriteResponseError(writer, http.StatusBadRequest, err.Error())
		return
	}
	service.AttachCanonicalRequestMetadata(&canonicalRequest, request.Header)

	if err := proxy.ValidateVisionLimits(canonicalRequest); err != nil {
		response.WriteResponseError(writer, http.StatusBadRequest, err.Error())
		return
	}

	var visionStore model.SettingsStore
	if s.DB != nil {
		visionStore = s.DB
	}
	if _, err := service.EvaluateVisionGate(request.Context(), visionStore, canonicalRequest); err != nil {
		response.WriteResponseError(writer, http.StatusInternalServerError, err.Error())
		return
	}

	policyResult, err := service.EvaluateCanonicalRequest(request.Context(), s.DB, canonicalRequest)
	if err != nil {
		response.WriteResponseError(writer, http.StatusInternalServerError, "policy: "+err.Error())
		return
	}
	sessionCanonicalRequest, err := s.Deps.Sessions.BuildCanonicalRequest(request.Context(), sessionID, policyResult.PostPolicyRequest)
	if err != nil {
		response.WriteResponseError(writer, http.StatusInternalServerError, "sessions: "+err.Error())
		return
	}
	projectedRequest, projectedMessages, err := proxy.ProjectCanonicalToOpenAIRequest(sessionCanonicalRequest)
	if err != nil {
		response.WriteResponseError(writer, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateChatRequest(&projectedRequest); err != nil {
		response.WriteResponseError(writer, http.StatusBadRequest, err.Error())
		return
	}

	messages := projectedMessages
	modelKey, err := s.Deps.Models.ResolveChatModel(request.Context(), projectedRequest.Model)
	if err != nil {
		response.WriteResponseError(writer, http.StatusBadRequest, "resolve model: "+err.Error())
		return
	}

	if s.accountRoutingEnabled() {
		s.handleAccountRoutedResponses(
			writer, request, respReq, projectedRequest, sessionCanonicalRequest,
			canonicalRequest, policyResult.PostPolicyRequest, sessionID, messages, modelKey,
		)
		return
	}

	snapshot, err := s.Deps.Credentials.Current(request.Context())
	if err != nil {
		response.WriteResponseError(writer, http.StatusInternalServerError, "credentials: "+err.Error())
		return
	}

	if s.Deps.Uploader != nil {
		imageURLs, err := s.uploadImagesFromCanonicalRequest(request.Context(), snapshot, sessionCanonicalRequest)
		if err != nil {
			response.WriteResponseError(writer, http.StatusInternalServerError, err.Error())
			return
		}
		if len(imageURLs) > 0 {
			sessionCanonicalRequest.Metadata = service.CloneMetadataMap(sessionCanonicalRequest.Metadata)
			if sessionCanonicalRequest.Metadata == nil {
				sessionCanonicalRequest.Metadata = map[string]any{}
			}
			sessionCanonicalRequest.Metadata["image_urls"] = imageURLs
			sessionCanonicalRequest.Metadata["is_vl"] = true
		}
	}

	remoteRequest, err := s.Deps.Builder.BuildCanonical(sessionCanonicalRequest, modelKey)
	if err != nil {
		response.WriteResponseError(writer, http.StatusInternalServerError, "build request: "+err.Error())
		return
	}

	stream, err := s.Deps.Transport.StreamChat(request.Context(), remoteRequest, snapshot)
	if err != nil {
		response.WriteResponseError(writer, http.StatusBadGateway, "upstream: "+err.Error())
		return
	}
	defer stream.Close()

	responseID := "resp_" + remoteRequest.RequestID
	traceID := proxy.NewUUID()

	if !respReq.Stream {
		s.nonStreamResponse(writer, request.Context(), stream, responseID, projectedRequest, remoteRequest,
			sessionID, messages, canonicalRequest, policyResult.PostPolicyRequest,
			sessionCanonicalRequest, traceID, respReq)
	} else {
		s.streamResponse(writer, request, stream, responseID, projectedRequest, remoteRequest,
			sessionID, messages, canonicalRequest, policyResult.PostPolicyRequest,
			sessionCanonicalRequest, traceID, respReq)
	}
}

// ── Non-stream response helper ───────────────────────────────────

func (s *Server) nonStreamResponse(
	writer http.ResponseWriter,
	ctx context.Context,
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
	respReq proxy.OpenAIResponseRequest,
) {
	var contentBuilder strings.Builder
	var reasoningBuilder strings.Builder
	var rawSSELines []string
	var promptTokens, completionTokens, totalTokens int
	var toolCalls []proxy.ToolCall

	err := proxy.ScanSSEWithLines(stream, func(line string) error {
		rawSSELines = append(rawSSELines, line)
		return nil
	}, func(event proxy.SSEEvent) error {
		if event.Usage != nil {
			promptTokens = event.Usage.PromptTokens
			completionTokens = event.Usage.CompletionTokens
			totalTokens = event.Usage.TotalTokens
		}
		contentBuilder.WriteString(event.Content)
		reasoningBuilder.WriteString(event.ReasoningContent)
		toolCalls = append(toolCalls, event.ToolCalls...)
		return nil
	})
	if err != nil {
		response.WriteResponseError(writer, http.StatusBadGateway, "upstream: "+err.Error())
		return
	}

	output := buildResponseOutput(responseID, contentBuilder.String(), reasoningBuilder.String(), toolCalls)
	if len(output) == 0 {
		output = append(output, proxy.ResponseOutputItem{
			ID:     responseID + "_msg_0",
			Type:   "message",
			Status: "completed",
			Role:   "assistant",
			Content: []proxy.ResponseOutputContent{{
				Type: "output_text",
				Text: "",
			}},
		})
	}

	if promptTokens <= 0 && completionTokens <= 0 {
		promptTokens = contentBuilder.Len()/4 + reasoningBuilder.Len()/4
		completionTokens = promptTokens
		totalTokens = promptTokens + completionTokens
	}

	assistant := proxy.Message{
		Role:    "assistant",
		Content: buildAssistantContent(contentBuilder.String(), reasoningBuilder.String()),
	}
	_ = s.Deps.Sessions.SaveCanonicalResponse(ctx, sessionID, sessionCanonicalRequest, assistant)

	service.PersistCanonicalExecutionRecord(
		ctx, s.DB, s.Deps.Now(), s.StoreExecutionLogs, traceID,
		prePolicyRequest.Protocol, "/v1/responses",
		prePolicyRequest, postPolicyRequest, sessionCanonicalRequest,
		chatRequest, messages, assistant, remoteRequest,
		rawSSELines, promptTokens, completionTokens, totalTokens,
	)
	if s.TokenStats != nil {
		_ = s.TokenStats.AddTokens(ctx, totalTokens)
	}

	builtinTools, _ := prePolicyRequest.Metadata["openai_builtin_tools"].([]string)
	output = appendBuiltinToolOutput(output, responseID, builtinTools)

	response.WriteJSON(writer, http.StatusOK, proxy.OpenAIResponse{
		ID:                 responseID,
		Object:             "response",
		CreatedAt:          s.Deps.Now().Unix(),
		Status:             "completed",
		Model:              chatRequest.Model,
		Output:             output,
		Instructions:       respReq.Instructions,
		MaxOutputTokens:    respReq.MaxOutputTokens,
		Temperature:        respReq.Temperature,
		TopP:               respReq.TopP,
		PreviousResponseID: respReq.PreviousResponseID,
		Usage: &proxy.OpenAIResponseUsage{
			InputTokens:  promptTokens,
			OutputTokens: completionTokens,
			TotalTokens:  totalTokens,
		},
	})
}

// ── Response output builders ─────────────────────────────────────

func buildResponseOutput(
	responseID string,
	content string,
	reasoning string,
	toolCalls []proxy.ToolCall,
) []proxy.ResponseOutputItem {
	var output []proxy.ResponseOutputItem
	idx := 0

	if reasoning != "" {
		output = append(output, proxy.ResponseOutputItem{
			ID:     fmt.Sprintf("%s_reasoning_%d", responseID, idx),
			Type:   "reasoning",
			Status: "completed",
			Summary: []proxy.ResponseOutputContent{{
				Type: "output_text",
				Text: reasoning,
			}},
		})
		idx++
	}

	if content != "" || len(toolCalls) == 0 {
		output = append(output, proxy.ResponseOutputItem{
			ID:     fmt.Sprintf("%s_msg_%d", responseID, idx),
			Type:   "message",
			Status: "completed",
			Role:   "assistant",
			Content: []proxy.ResponseOutputContent{{
				Type: "output_text",
				Text: content,
			}},
		})
		idx++
	}

	merged := mergeToolCallDeltas(toolCalls)
	for _, tc := range merged {
		output = append(output, proxy.ResponseOutputItem{
			ID:        fmt.Sprintf("%s_fc_%d", responseID, idx),
			Type:      "function_call",
			Status:    "completed",
			Name:      tc.Function.Name,
			Arguments: tc.Function.Arguments,
			CallID:    tc.ID,
		})
		idx++
	}

	return output
}

func appendBuiltinToolOutput(output []proxy.ResponseOutputItem, responseID string, builtinTypes []string) []proxy.ResponseOutputItem {
	for _, bt := range builtinTypes {
		output = append(output, proxy.ResponseOutputItem{
			ID:     fmt.Sprintf("%s_bt_%s", responseID, bt),
			Type:   "message",
			Status: "incomplete",
			Role:   "assistant",
			Content: []proxy.ResponseOutputContent{{
				Type: "output_text",
				Text: fmt.Sprintf("[Built-in tool %q is not available in this backend]", bt),
			}},
		})
	}
	return output
}

func buildAssistantContent(content string, reasoning string) string {
	if reasoning != "" {
		return "[thinking]" + reasoning + "[/thinking]" + content
	}
	return content
}

// ── Stream response ──────────────────────────────────────────────

func (s *Server) streamResponse(
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
	respReq proxy.OpenAIResponseRequest,
) {
	flusher, ok := writer.(http.Flusher)
	if !ok {
		response.WriteResponseError(writer, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.Header().Set("Connection", "keep-alive")

	// response.created
	response.WriteResponseSSE(writer, proxy.ResponseStreamEvent{
		Type: "response.created",
		Response: &proxy.OpenAIResponse{
			ID:           responseID,
			Object:       "response",
			Status:       "in_progress",
			CreatedAt:    s.Deps.Now().Unix(),
			Model:        chatRequest.Model,
			Instructions: respReq.Instructions,
		},
	})
	flusher.Flush()

	// response.in_progress
	response.WriteResponseSSE(writer, proxy.ResponseStreamEvent{
		Type: "response.in_progress",
		Response: &proxy.OpenAIResponse{
			ID:        responseID,
			Object:    "response",
			Status:    "in_progress",
			CreatedAt: s.Deps.Now().Unix(),
			Model:     chatRequest.Model,
		},
	})
	flusher.Flush()

	var contentBuilder strings.Builder
	var reasoningBuilder strings.Builder
	var rawSSELines []string
	var promptTokens, completionTokens, totalTokens int
	var hasReasoning, hasText bool
	outputIndex := 0
	contentIndex := 0
	itemStarted := false
	currentItemID := ""

	closeItem := func() {
		if !itemStarted {
			return
		}
		response.WriteResponseSSE(writer, proxy.ResponseStreamEvent{
			Type:        "response.output_item.done",
			ItemID:      currentItemID,
			OutputIndex: outputIndex - 1,
			Item: &proxy.ResponseOutputItem{
				ID:     currentItemID,
				Status: "completed",
			},
		})
		flusher.Flush()
		itemStarted = false
	}

	startItem := func(itemType string) {
		if itemStarted {
			closeItem()
		}
		currentItemID = fmt.Sprintf("%s_%s_%d", responseID, itemType, outputIndex)
		outputIndex++
		itemStarted = true

		response.WriteResponseSSE(writer, proxy.ResponseStreamEvent{
			Type:        "response.output_item.added",
			OutputIndex: outputIndex - 1,
			Item: &proxy.ResponseOutputItem{
				ID:     currentItemID,
				Type:   itemType,
				Status: "in_progress",
			},
		})
		flusher.Flush()
	}

	startContentPart := func() {
		response.WriteResponseSSE(writer, proxy.ResponseStreamEvent{
			Type:         "response.content_part.added",
			ItemID:       currentItemID,
			OutputIndex:  outputIndex - 1,
			ContentIndex: contentIndex,
			Part: &proxy.ResponseOutputItem{
				Type: "message",
				Content: []proxy.ResponseOutputContent{{
					Type: "output_text",
				}},
			},
		})
		flusher.Flush()
	}

	closeContentPart := func() {
		response.WriteResponseSSE(writer, proxy.ResponseStreamEvent{
			Type:         "response.content_part.done",
			ItemID:       currentItemID,
			OutputIndex:  outputIndex - 1,
			ContentIndex: contentIndex,
		})
		flusher.Flush()
		contentIndex++
	}

	err := proxy.ScanSSEWithLines(stream, func(line string) error {
		rawSSELines = append(rawSSELines, line)
		return nil
	}, func(event proxy.SSEEvent) error {
		if event.Usage != nil {
			promptTokens = event.Usage.PromptTokens
			completionTokens = event.Usage.CompletionTokens
			totalTokens = event.Usage.TotalTokens
		}
		if event.Done {
			return nil
		}

		if event.ReasoningContent != "" {
			if !hasReasoning {
				startItem("reasoning")
				hasReasoning = true
			}
			reasoningBuilder.WriteString(event.ReasoningContent)
			response.WriteResponseSSE(writer, proxy.ResponseStreamEvent{
				Type:        "response.reasoning_summary_part.added",
				ItemID:      currentItemID,
				OutputIndex: outputIndex - 1,
			})
			flusher.Flush()
			return nil
		}

		if event.Content != "" {
			if !hasText {
				closeItem()
				startItem("message")
				startContentPart()
				hasText = true
			}
			contentBuilder.WriteString(event.Content)
			response.WriteResponseSSE(writer, proxy.ResponseStreamEvent{
				Type:         "response.output_text.delta",
				ItemID:       currentItemID,
				OutputIndex:  outputIndex - 1,
				ContentIndex: contentIndex,
				Delta:        event.Content,
			})
			flusher.Flush()
			return nil
		}

		for _, tc := range event.ToolCalls {
			isNew := tc.ID != "" || tc.Function.Name != ""
			if isNew {
				closeItem()
				closeContentPart()
				contentIndex = 0
				startItem("function_call")
				hasText = false
			}
			if tc.Function.Arguments != "" {
				response.WriteResponseSSE(writer, proxy.ResponseStreamEvent{
					Type:        "response.function_call_arguments.delta",
					ItemID:      currentItemID,
					OutputIndex: outputIndex - 1,
					Delta:       tc.Function.Arguments,
				})
				flusher.Flush()
			}
		}
		return nil
	})

	if err != nil {
		closeContentPart()
		closeItem()
		var errorBuf strings.Builder
		errorBuf.WriteString(`{"type":"error","message":"`)
		errorBuf.WriteString(err.Error())
		errorBuf.WriteString(`"}`)
		fmt.Fprintf(writer, "event: error\ndata: %s\n\n", errorBuf.String())
		flusher.Flush()
		return
	}

	if hasText {
		// output_text.done
		response.WriteResponseSSE(writer, proxy.ResponseStreamEvent{
			Type:         "response.output_text.done",
			ItemID:       currentItemID,
			OutputIndex:  outputIndex - 1,
			ContentIndex: contentIndex,
			Delta:        "",
		})
		flusher.Flush()

		// content_part.done
		closeContentPart()
	}

	if hasReasoning {
		response.WriteResponseSSE(writer, proxy.ResponseStreamEvent{
			Type:        "response.reasoning_summary_part.done",
			ItemID:      responseID + "_reasoning_0",
			OutputIndex: outputIndex - 1,
		})
		flusher.Flush()
	}

	closeItem()

	if completionTokens <= 0 {
		completionTokens = (contentBuilder.Len() + reasoningBuilder.Len()) / 4
		if completionTokens == 0 {
			completionTokens = 1
		}
	}
	if promptTokens <= 0 {
		promptTokens = completionTokens * 2
	}
	totalTokens = promptTokens + completionTokens

	// response.completed
	response.WriteResponseSSE(writer, proxy.ResponseStreamEvent{
		Type: "response.completed",
		Response: &proxy.OpenAIResponse{
			ID:     responseID,
			Object: "response",
			Status: "completed",
			Model:  chatRequest.Model,
			Usage: &proxy.OpenAIResponseUsage{
				InputTokens:  promptTokens,
				OutputTokens: completionTokens,
				TotalTokens:  totalTokens,
			},
		},
	})
	flusher.Flush()

	assistant := proxy.Message{
		Role:    "assistant",
		Content: buildAssistantContent(contentBuilder.String(), reasoningBuilder.String()),
	}
	_ = s.Deps.Sessions.SaveCanonicalResponse(request.Context(), sessionID, sessionCanonicalRequest, assistant)

	service.PersistCanonicalExecutionRecord(
		request.Context(), s.DB, s.Deps.Now(), s.StoreExecutionLogs, traceID,
		prePolicyRequest.Protocol, "/v1/responses",
		prePolicyRequest, postPolicyRequest, sessionCanonicalRequest,
		chatRequest, messages, assistant, remoteRequest,
		rawSSELines, promptTokens, completionTokens, totalTokens,
	)
	if s.TokenStats != nil {
		_ = s.TokenStats.AddTokens(request.Context(), totalTokens)
	}
}

func (s *Server) handleAccountRoutedResponses(
	writer http.ResponseWriter,
	request *http.Request,
	respReq proxy.OpenAIResponseRequest,
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
		response.WriteResponseError(writer, http.StatusInternalServerError, "routing: "+err.Error())
		return
	}
	attachAccountRoutingMetadata(&prePolicyRequest, account)
	attachAccountRoutingMetadata(&postPolicyRequest, account)
	attachAccountRoutingMetadata(&sessionCanonicalRequest, account)

	imageURLs, err := s.uploadImagesWithAdapter(request.Context(), adapter, account, sessionCanonicalRequest)
	if err != nil {
		response.WriteResponseError(writer, http.StatusInternalServerError, "upload: "+err.Error())
		return
	}
	if len(imageURLs) > 0 {
		sessionCanonicalRequest.Metadata = service.CloneMetadataMap(sessionCanonicalRequest.Metadata)
		if sessionCanonicalRequest.Metadata == nil {
			sessionCanonicalRequest.Metadata = map[string]any{}
		}
		sessionCanonicalRequest.Metadata["image_urls"] = imageURLs
		sessionCanonicalRequest.Metadata["is_vl"] = true
	}

	remoteRequest, err := adapter.BuildChatRequest(request.Context(), sessionCanonicalRequest, modelKey, account)
	if err != nil {
		response.WriteResponseError(writer, http.StatusInternalServerError, "build request: "+err.Error())
		return
	}
	stream, err := adapter.StreamChat(request.Context(), remoteRequest, account)
	if err != nil {
		response.WriteResponseError(writer, http.StatusBadGateway, "upstream: "+err.Error())
		return
	}
	defer stream.Close()

	responseID := "resp_" + remoteRequest.RequestID
	traceID := proxy.NewUUID()

	if !respReq.Stream {
		s.nonStreamResponse(writer, request.Context(), stream, responseID, projectedRequest, remoteRequest,
			sessionID, messages, prePolicyRequest, postPolicyRequest,
			sessionCanonicalRequest, traceID, respReq)
	} else {
		s.streamResponse(writer, request, stream, responseID, projectedRequest, remoteRequest,
			sessionID, messages, prePolicyRequest, postPolicyRequest,
			sessionCanonicalRequest, traceID, respReq)
	}
}
