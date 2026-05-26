package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rizxfrog/oh-my-api/internal/api/model"
	"github.com/rizxfrog/oh-my-api/internal/api/response"
	"github.com/rizxfrog/oh-my-api/internal/api/service"
	"github.com/rizxfrog/oh-my-api/internal/proxy"
)

// ── Request parsing & validation ─────────────────────────────────

func decodeChatRequest(writer http.ResponseWriter, request *http.Request) (proxy.OpenAIChatRequest, error) {
	body := http.MaxBytesReader(writer, request.Body, 1<<20)
	defer body.Close()

	var chatRequest proxy.OpenAIChatRequest
	if err := json.NewDecoder(body).Decode(&chatRequest); err != nil {
		return proxy.OpenAIChatRequest{}, err
	}
	return chatRequest, nil
}

func validateChatRequest(request *proxy.OpenAIChatRequest) error {
	if len(request.Messages) == 0 {
		return errors.New("messages must not be empty")
	}
	for i := range request.Messages {
		message := &request.Messages[i]
		switch message.Role {
		case "system", "user":
			if message.Content == "" && len(message.Parts) == 0 {
				return errors.New("message content must not be empty")
			}
		case "assistant":
			if len(message.ToolCalls) > 0 {
				filtered := message.ToolCalls[:0]
				for _, tc := range message.ToolCalls {
					if tc.Function.Name != "" {
						filtered = append(filtered, tc)
					}
				}
				message.ToolCalls = filtered
			}
			if message.Content == "" && len(message.ToolCalls) == 0 {
				return errors.New("assistant message must have content or tool_calls")
			}
		case "tool":
			if message.ToolCallID == "" {
				return errors.New("tool message must have tool_call_id")
			}
		default:
			return fmt.Errorf("unsupported role %q", message.Role)
		}
	}
	return nil
}

// ── SSE helpers ──────────────────────────────────────────────────

func collectSSEContentWithUsage(reader io.Reader) (content string, rawLines []string, promptTokens, completionTokens, totalTokens int, err error) {
	var builder strings.Builder
	err = proxy.ScanSSEWithLines(reader, func(line string) error {
		rawLines = append(rawLines, line)
		return nil
	}, func(event proxy.SSEEvent) error {
		if event.Usage != nil {
			promptTokens = event.Usage.PromptTokens
			completionTokens = event.Usage.CompletionTokens
			totalTokens = event.Usage.TotalTokens
		}
		builder.WriteString(event.Content)
		return nil
	})
	if err != nil {
		return "", nil, 0, 0, 0, err
	}
	if rawLines == nil {
		rawLines = []string{}
	}
	return builder.String(), rawLines, promptTokens, completionTokens, totalTokens, nil
}

// ── Chat completion handler ──────────────────────────────────────

func (s *Server) HandleChatCompletions(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		response.WriteMethodNotAllowed(writer, http.MethodPost)
		return
	}

	chatRequest, err := decodeChatRequest(writer, request)
	if err != nil {
		response.WriteOpenAIError(writer, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateChatRequest(&chatRequest); err != nil {
		response.WriteOpenAIError(writer, http.StatusBadRequest, err.Error())
		return
	}

	sessionID := service.RequestSessionID(request, chatRequest.ExtraBody.SessionID)
	canonicalRequest, err := proxy.CanonicalizeOpenAIRequest(chatRequest, sessionID)
	if err != nil {
		response.WriteOpenAIError(writer, http.StatusBadRequest, err.Error())
		return
	}
	service.AttachCanonicalRequestMetadata(&canonicalRequest, request.Header)

	if err := proxy.ValidateVisionLimits(canonicalRequest); err != nil {
		response.WriteOpenAIInvalidImage(writer, err.Error())
		return
	}
	var visionStore model.SettingsStore
	if s.DB != nil {
		visionStore = s.DB
	}
	if _, err := service.EvaluateVisionGate(request.Context(), visionStore, canonicalRequest); err != nil {
		if service.IsVisionNotImplemented(err) {
			response.WriteOpenAIVisionNotImplemented(writer)
			return
		}
		response.WriteMappedError(writer, err)
		return
	}

	policyResult, err := service.EvaluateCanonicalRequest(request.Context(), s.DB, canonicalRequest)
	if err != nil {
		response.WriteMappedError(writer, err)
		return
	}
	sessionCanonicalRequest, err := s.Deps.Sessions.BuildCanonicalRequest(request.Context(), sessionID, policyResult.PostPolicyRequest)
	if err != nil {
		response.WriteMappedError(writer, err)
		return
	}
	projectedRequest, projectedMessages, err := proxy.ProjectCanonicalToOpenAIRequest(sessionCanonicalRequest)
	if err != nil {
		response.WriteOpenAIError(writer, http.StatusBadRequest, err.Error())
		return
	}
	if err := validateChatRequest(&projectedRequest); err != nil {
		response.WriteOpenAIError(writer, http.StatusBadRequest, err.Error())
		return
	}

	messages := projectedMessages
	modelKey, err := s.Deps.Models.ResolveChatModel(request.Context(), projectedRequest.Model)
	if err != nil {
		response.WriteMappedError(writer, err)
		return
	}
	if s.accountRoutingEnabled() {
		s.handleAccountRoutedChat(
			writer,
			request,
			projectedRequest,
			sessionCanonicalRequest,
			canonicalRequest,
			policyResult.PostPolicyRequest,
			sessionID,
			messages,
			modelKey,
		)
		return
	}
	credential, err := s.Deps.Credentials.Current(request.Context())
	if err != nil {
		response.WriteMappedError(writer, err)
		return
	}

	if s.Deps.Uploader != nil {
		imageURLs, err := s.uploadImagesFromCanonicalRequest(request.Context(), credential, sessionCanonicalRequest)
		if err != nil {
			response.WriteMappedError(writer, err)
			return
		}
		if len(imageURLs) > 0 {
			sessionCanonicalRequest.Metadata["image_urls"] = imageURLs
			sessionCanonicalRequest.Metadata["is_vl"] = true
		}
	}

	remoteRequest, err := s.Deps.Builder.BuildCanonical(sessionCanonicalRequest, modelKey)
	if err != nil {
		response.WriteMappedError(writer, err)
		return
	}
	stream, err := s.Deps.Transport.StreamChat(request.Context(), remoteRequest, credential)
	if err != nil {
		response.WriteMappedError(writer, err)
		return
	}
	defer stream.Close()

	traceID := proxy.NewUUID()
	if projectedRequest.Stream {
		s.streamChatResponse(
			writer,
			request,
			projectedRequest,
			remoteRequest,
			sessionID,
			messages,
			stream,
			canonicalRequest,
			policyResult.PostPolicyRequest,
			sessionCanonicalRequest,
			traceID,
		)
		return
	}
	s.writeNonStreamResponse(
		request.Context(),
		writer,
		projectedRequest,
		remoteRequest,
		sessionID,
		messages,
		stream,
		canonicalRequest,
		policyResult.PostPolicyRequest,
		sessionCanonicalRequest,
		traceID,
	)
}

func (s *Server) handleAccountRoutedChat(
	writer http.ResponseWriter,
	request *http.Request,
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
		response.WriteMappedError(writer, err)
		return
	}
	attachAccountRoutingMetadata(&prePolicyRequest, account)
	attachAccountRoutingMetadata(&postPolicyRequest, account)
	attachAccountRoutingMetadata(&sessionCanonicalRequest, account)
	imageURLs, err := s.uploadImagesWithAdapter(request.Context(), adapter, account, sessionCanonicalRequest)
	if err != nil {
		response.WriteMappedError(writer, err)
		return
	}
	if len(imageURLs) > 0 {
		sessionCanonicalRequest.Metadata["image_urls"] = imageURLs
		sessionCanonicalRequest.Metadata["is_vl"] = true
	}

	remoteRequest, err := adapter.BuildChatRequest(request.Context(), sessionCanonicalRequest, modelKey, account)
	if err != nil {
		response.WriteMappedError(writer, err)
		return
	}
	stream, err := adapter.StreamChat(request.Context(), remoteRequest, account)
	if err != nil {
		response.WriteMappedError(writer, err)
		return
	}
	defer stream.Close()

	traceID := proxy.NewUUID()
	if projectedRequest.Stream {
		s.streamChatResponse(
			writer,
			request,
			projectedRequest,
			remoteRequest,
			sessionID,
			messages,
			stream,
			prePolicyRequest,
			postPolicyRequest,
			sessionCanonicalRequest,
			traceID,
		)
		return
	}
	s.writeNonStreamResponse(
		request.Context(),
		writer,
		projectedRequest,
		remoteRequest,
		sessionID,
		messages,
		stream,
		prePolicyRequest,
		postPolicyRequest,
		sessionCanonicalRequest,
		traceID,
	)
}

// ── Non-stream response ──────────────────────────────────────────

func (s *Server) writeNonStreamResponse(
	ctx context.Context,
	writer http.ResponseWriter,
	request proxy.OpenAIChatRequest,
	remoteRequest proxy.RemoteChatRequest,
	sessionID string,
	messages []proxy.Message,
	stream io.Reader,
	prePolicyRequest proxy.CanonicalRequest,
	postPolicyRequest proxy.CanonicalRequest,
	sessionCanonicalRequest proxy.CanonicalRequest,
	traceID string,
) {
	startTime := s.Deps.Now()
	content, rawSSELines, promptTokens, completionTokens, totalTokens, err := collectSSEContentWithUsage(stream)
	if err != nil {
		response.WriteMappedError(writer, err)
		return
	}
	assistant := proxy.Message{
		Role:    "assistant",
		Content: content,
	}
	if err := s.Deps.Sessions.SaveCanonicalResponse(context.Background(), sessionID, sessionCanonicalRequest, assistant); err != nil {
		response.WriteMappedError(writer, err)
		return
	}
	service.PersistCanonicalExecutionRecord(
		ctx,
		s.DB,
		s.Deps.Now(),
		s.StoreExecutionLogs,
		traceID,
		prePolicyRequest.Protocol,
		"/v1/chat/completions",
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
	if s.RequestStats != nil {
		ttftMs := int(s.Deps.Now().Sub(startTime).Milliseconds())
		_ = s.RequestStats.RecordRequest(ctx, true, ttftMs)
	}

	finishReason := "stop"
	response.WriteJSON(writer, http.StatusOK, model.ChatCompletionResponse{
		ID:      "chatcmpl-" + remoteRequest.RequestID,
		Object:  "chat.completion",
		Created: s.Deps.Now().Unix(),
		Model:   request.Model,
		Choices: []model.ChatCompletionChoice{
			{
				Index: 0,
				Message: &proxy.Message{
					Role:    "assistant",
					Content: content,
				},
				FinishReason: &finishReason,
			},
		},
		Usage: &model.OpenAIUsage{
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      totalTokens,
		},
	})
}

// ── Stream response ──────────────────────────────────────────────

func (s *Server) streamChatResponse(
	writer http.ResponseWriter,
	request *http.Request,
	chatRequest proxy.OpenAIChatRequest,
	remoteRequest proxy.RemoteChatRequest,
	sessionID string,
	messages []proxy.Message,
	stream io.Reader,
	prePolicyRequest proxy.CanonicalRequest,
	postPolicyRequest proxy.CanonicalRequest,
	sessionCanonicalRequest proxy.CanonicalRequest,
	traceID string,
) {
	startTime := s.Deps.Now()
	var firstTokenTime time.Time
	flusher, ok := writer.(http.Flusher)
	if !ok {
		response.WriteOpenAIError(writer, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("Cache-Control", "no-cache")
	writer.Header().Set("Connection", "keep-alive")

	responseID := "chatcmpl-" + remoteRequest.RequestID
	if err := response.WriteSSEChunk(writer, model.ChatCompletionResponse{
		ID:      responseID,
		Object:  "chat.completion.chunk",
		Created: s.Deps.Now().Unix(),
		Model:   chatRequest.Model,
		Choices: []model.ChatCompletionChoice{{
			Index: 0,
			Delta: &model.DeltaPayload{Role: "assistant"},
		}},
	}); err != nil {
		return
	}
	flusher.Flush()

	type pendingToolCall struct {
		id   string
		typ  string
		name string
		args strings.Builder
	}
	pendingTCs := map[int]*pendingToolCall{}

	emitPending := func(p *pendingToolCall) error {
		tc := proxy.ToolCall{
			Index: 0,
			ID:    p.id,
			Type:  p.typ,
			Function: proxy.FunctionCall{
				Name:      p.name,
				Arguments: p.args.String(),
			},
		}
		if tc.ID == "" {
			tc.ID = "call_" + remoteRequest.RequestID + "_0"
		}
		choice := model.ChatCompletionChoice{Index: 0}
		choice.Delta = &model.DeltaPayload{
			Role:      "assistant",
			ToolCalls: []proxy.ToolCall{tc},
		}
		return response.WriteSSEChunk(writer, model.ChatCompletionResponse{
			ID:      responseID,
			Object:  "chat.completion.chunk",
			Created: s.Deps.Now().Unix(),
			Model:   chatRequest.Model,
			Choices: []model.ChatCompletionChoice{choice},
		})
	}

	var contentBuilder strings.Builder
	var rawSSELines []string
	var promptTokens, completionTokens, totalTokens int
	emittedToolCalls := false
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
		if event.Content == "" && len(event.ToolCalls) == 0 {
			return nil
		}
		if event.Content != "" {
			if firstTokenTime.IsZero() {
				firstTokenTime = s.Deps.Now()
			}
			for _, p := range pendingTCs {
				if err := emitPending(p); err != nil {
					return err
				}
				emittedToolCalls = true
				flusher.Flush()
			}
			pendingTCs = map[int]*pendingToolCall{}

			contentBuilder.WriteString(event.Content)
			choice := model.ChatCompletionChoice{Index: 0}
			choice.Delta = &model.DeltaPayload{Content: event.Content}
			if err := response.WriteSSEChunk(writer, model.ChatCompletionResponse{
				ID:      responseID,
				Object:  "chat.completion.chunk",
				Created: s.Deps.Now().Unix(),
				Model:   chatRequest.Model,
				Choices: []model.ChatCompletionChoice{choice},
			}); err != nil {
				return err
			}
			flusher.Flush()
			return nil
		}
		for _, tc := range event.ToolCalls {
			idx := tc.Index
			p, exists := pendingTCs[idx]
			isNew := tc.ID != "" || tc.Function.Name != ""
			if !isNew && exists {
				p.args.WriteString(tc.Function.Arguments)
			} else {
				if exists {
					if err := emitPending(p); err != nil {
						return err
					}
					emittedToolCalls = true
					flusher.Flush()
				}
				p = &pendingToolCall{
					id:   tc.ID,
					typ:  tc.Type,
					name: tc.Function.Name,
				}
				p.args.WriteString(tc.Function.Arguments)
				pendingTCs[idx] = p
			}
		}
		return nil
	})
	if err == nil {
		for _, p := range pendingTCs {
			if err := emitPending(p); err != nil {
				break
			}
			emittedToolCalls = true
			flusher.Flush()
		}
	}
	if err != nil {
		_, _ = fmt.Fprintf(writer, "data: {\"error\":{\"message\":%q}}\n\n", err.Error())
		flusher.Flush()
		return
	}

	assistant := proxy.Message{
		Role:    "assistant",
		Content: contentBuilder.String(),
	}
	if err := s.Deps.Sessions.SaveCanonicalResponse(request.Context(), sessionID, sessionCanonicalRequest, assistant); err != nil {
		return
	}
	service.PersistCanonicalExecutionRecord(
		request.Context(),
		s.DB,
		s.Deps.Now(),
		s.StoreExecutionLogs,
		traceID,
		prePolicyRequest.Protocol,
		"/v1/chat/completions",
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
	if s.RequestStats != nil {
		ttftMs := int(firstTokenTime.Sub(startTime).Milliseconds())
		_ = s.RequestStats.RecordRequest(request.Context(), true, ttftMs)
	}

	finishReason := "stop"
	if emittedToolCalls {
		finishReason = "tool_calls"
	}
	_ = response.WriteSSEChunk(writer, model.ChatCompletionResponse{
		ID:      responseID,
		Object:  "chat.completion.chunk",
		Created: s.Deps.Now().Unix(),
		Model:   chatRequest.Model,
		Choices: []model.ChatCompletionChoice{{
			Index:        0,
			Delta:        &model.DeltaPayload{},
			FinishReason: &finishReason,
		}},
	})
	_, _ = io.WriteString(writer, "data: [DONE]\n\n")
	flusher.Flush()
}

// ── Admin auth ───────────────────────────────────────────────────

func (s *Server) isAdminAuthorized(request *http.Request) bool {
	if s.Deps.AdminToken == "" {
		return true
	}
	if token := strings.TrimSpace(request.Header.Get("X-Admin-Token")); token == s.Deps.AdminToken {
		return true
	}
	authorization := strings.TrimSpace(request.Header.Get("Authorization"))
	return authorization == "Bearer "+s.Deps.AdminToken
}

func parseIntOr(s string, def int) int {
	if v, err := strconv.Atoi(s); err == nil {
		return v
	}
	return def
}
