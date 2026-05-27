package proxy

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

type CodeBuddyClient struct {
	baseURL    string
	httpClient *http.Client
	keywords   map[string]string
}

func NewCodeBuddyClient(baseURL string) *CodeBuddyClient {
	return &CodeBuddyClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			},
			Timeout: 300 * time.Second,
		},
		keywords: map[string]string{
			"Claude Code":                         "CodeBuddy Code",
			"Anthropic's official CLI for Claude": "Tencent's official CLI for CodeBuddy",
			"Claude":                              "CodeBuddy",
			"Anthropic":                           "Tencent",
			"https://github.com/anthropics/claude-code/issues": "https://cnb.cool/codebuddy/codebuddy-code/-/issues",
		},
	}
}

func (c *CodeBuddyClient) SendChat(ctx context.Context, apiKey string, req OpenAIChatRequest) (io.ReadCloser, error) {
	req.Stream = true
	req.Messages = c.applyKeywordReplacement(req.Messages)
	body, _ := json.Marshal(req)
	httpReq, _ := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v2/chat/completions", bytes.NewReader(body))
	for k, v := range c.buildHeaders(apiKey) {
		httpReq.Header[k] = v
	}
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		resp.Body.Close()
		return nil, &UpstreamHTTPError{StatusCode: resp.StatusCode, Body: "upstream error"}
	}
	return resp.Body, nil
}

func (c *CodeBuddyClient) buildHeaders(apiKey string) map[string][]string {
	return map[string][]string{
		"Accept":                      {"application/json"},
		"Content-Type":                {"application/json"},
		"Authorization":               {"Bearer " + apiKey},
		"X-API-Key":                   {apiKey},
		"X-IDE-Type":                  {"CLI"},
		"X-IDE-Name":                  {"CLI"},
		"X-IDE-Version":               {"1.0.7"},
		"User-Agent":                  {"CLI/1.0.7 CodeBuddy/1.0.7"},
		"X-Agent-Intent":              {"craft"},
		"X-Product":                   {"SaaS"},
		"x-stainless-arch":            {"x64"},
		"x-stainless-lang":            {"js"},
		"x-stainless-os":              {"Windows"},
		"x-stainless-package-version": {"5.10.1"},
		"x-stainless-runtime":         {"node"},
		"x-stainless-runtime-version": {"v22.13.1"},
	}
}

func (c *CodeBuddyClient) applyKeywordReplacement(messages []Message) []Message {
	result := make([]Message, len(messages))
	copy(result, messages)
	for i := range result {
		if result[i].Role == "system" {
			content := result[i].Content
			for old, new_ := range c.keywords {
				content = strings.ReplaceAll(content, old, new_)
			}
			result[i].Content = content
		}
	}
	return result
}
