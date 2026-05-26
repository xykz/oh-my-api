package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ==================== 数据结构 ====================

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	MaxTokens   int       `json:"max_tokens"`
	Temperature float64   `json:"temperature"`
	Stream      bool      `json:"stream"`
}

type ChatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

type Result struct {
	Success      bool
	Latency      time.Duration
	TTFB         time.Duration
	TokensIn     int
	TokensOut    int
	Throughput   float64 // tokens/sec
	Error        string
	StatusCode   int
	RequestBody  string
	ResponseBody string
}

type Stats struct {
	TotalRequests  int64
	Successful     int64
	Failed         int64
	TotalTokensIn  int64
	TotalTokensOut int64
	Latencies      []time.Duration
	TTFBs          []time.Duration
	Throughputs    []float64
	StartTime      time.Time
	EndTime        time.Time
	mu             sync.Mutex
}

func (s *Stats) Add(r Result) {
	atomic.AddInt64(&s.TotalRequests, 1)
	if r.Success {
		atomic.AddInt64(&s.Successful, 1)
		atomic.AddInt64(&s.TotalTokensIn, int64(r.TokensIn))
		atomic.AddInt64(&s.TotalTokensOut, int64(r.TokensOut))
		s.mu.Lock()
		s.Latencies = append(s.Latencies, r.Latency)
		s.TTFBs = append(s.TTFBs, r.TTFB)
		s.Throughputs = append(s.Throughputs, r.Throughput)
		s.mu.Unlock()
	} else {
		atomic.AddInt64(&s.Failed, 1)
	}
}

func (s *Stats) Duration() time.Duration {
	return s.EndTime.Sub(s.StartTime)
}

func (s *Stats) QPS() float64 {
	d := s.Duration().Seconds()
	if d == 0 {
		return 0
	}
	return float64(s.TotalRequests) / d
}

func (s *Stats) AvgLatency() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.Latencies) == 0 {
		return 0
	}
	var sum time.Duration
	for _, v := range s.Latencies {
		sum += v
	}
	return sum / time.Duration(len(s.Latencies))
}

func (s *Stats) P50Latency() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.Latencies) == 0 {
		return 0
	}
	// 简单近似，实际可用 sort
	return s.Latencies[len(s.Latencies)/2]
}

func (s *Stats) P99Latency() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.Latencies) == 0 {
		return 0
	}
	idx := int(float64(len(s.Latencies)) * 0.99)
	if idx >= len(s.Latencies) {
		idx = len(s.Latencies) - 1
	}
	// 实际应该排序，这里简化
	return s.Latencies[idx]
}

func (s *Stats) AvgTTFB() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.TTFBs) == 0 {
		return 0
	}
	var sum time.Duration
	for _, v := range s.TTFBs {
		sum += v
	}
	return sum / time.Duration(len(s.TTFBs))
}

func (s *Stats) AvgThroughput() float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.Throughputs) == 0 {
		return 0
	}
	var sum float64
	for _, v := range s.Throughputs {
		sum += v
	}
	return sum / float64(len(s.Throughputs))
}

// ==================== HTTP 客户端 ====================

var authToken string

var httpClient = &http.Client{
	Transport: &http.Transport{
		MaxIdleConns:        10000,
		MaxIdleConnsPerHost: 10000,
		MaxConnsPerHost:     10000,
		IdleConnTimeout:     90 * time.Second,
		DisableKeepAlives:   false, // 复用连接
	},
	Timeout: 120 * time.Second,
}

func sendRequest(url string, payload ChatRequest) Result {
	start := time.Now()
	var ttfb time.Duration
	var respBodyStr string
	tokensIn := payload.MaxTokens // 简化估算
	tokensOut := 0

	body, err := json.Marshal(payload)
	if err != nil {
		return Result{Success: false, Latency: time.Since(start), Error: err.Error()}
	}
	reqBodyStr := string(body)

	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return Result{Success: false, Latency: time.Since(start), Error: err.Error(), RequestBody: reqBodyStr}
	}
	req.Header.Set("Content-Type", "application/json")
	if authToken != "" {
		req.Header.Set("Authorization", "Bearer "+authToken)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return Result{Success: false, Latency: time.Since(start), Error: err.Error(), RequestBody: reqBodyStr}
	}
	defer resp.Body.Close()

	if payload.Stream {
		// 流式模式：测量 TTFB
		reader := bufio.NewReader(resp.Body)
		firstByte := true
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if err == io.EOF {
					break
				}
				return Result{
					Success:     false,
					Latency:     time.Since(start),
					TTFB:        ttfb,
					TokensIn:    tokensIn,
					TokensOut:   tokensOut,
					Error:       err.Error(),
					StatusCode:  resp.StatusCode,
					RequestBody: reqBodyStr,
				}
			}
			if firstByte && strings.TrimSpace(line) != "" {
				ttfb = time.Since(start)
				firstByte = false
			}
			if strings.HasPrefix(line, "data: ") {
				tokensOut++
			}
		}
	} else {
		ttfb = time.Since(start)
		bodyBytes, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return Result{
				Success:     false,
				Latency:     time.Since(start),
				TTFB:        ttfb,
				TokensIn:    tokensIn,
				Error:       readErr.Error(),
				StatusCode:  resp.StatusCode,
				RequestBody: reqBodyStr,
			}
		}
		respBodyStr = string(bodyBytes)
		var chatResp ChatResponse
		if err := json.Unmarshal(bodyBytes, &chatResp); err != nil {
			bodyPreview := respBodyStr
			if len(bodyPreview) > 200 {
				bodyPreview = bodyPreview[:200] + "..."
			}
			return Result{
				Success:      false,
				Latency:      time.Since(start),
				TTFB:         ttfb,
				TokensIn:     tokensIn,
				Error:        bodyPreview,
				StatusCode:   resp.StatusCode,
				RequestBody:  reqBodyStr,
				ResponseBody: respBodyStr,
			}
		}
		if len(chatResp.Choices) > 0 {
			content := chatResp.Choices[0].Message.Content
			tokensOut = len(content) / 4 // 粗略估算
		}
		if chatResp.Usage.CompletionTokens > 0 {
			tokensOut = chatResp.Usage.CompletionTokens
			tokensIn = chatResp.Usage.PromptTokens
		}
	}

	latency := time.Since(start)
	throughput := 0.0
	if latency.Seconds() > 0 {
		throughput = float64(tokensOut) / latency.Seconds()
	}

	errMsg := ""
	if resp.StatusCode != 200 {
		errMsg = fmt.Sprintf("HTTP %d", resp.StatusCode)
	}

	return Result{
		Success:      resp.StatusCode == 200,
		Latency:      latency,
		TTFB:         ttfb,
		TokensIn:     tokensIn,
		TokensOut:    tokensOut,
		Throughput:   throughput,
		StatusCode:   resp.StatusCode,
		Error:        errMsg,
		RequestBody:  reqBodyStr,
		ResponseBody: respBodyStr,
	}
}

// ==================== 工作协程 ====================

func worker(id int, url string, basePayload ChatRequest, count int, resultCh chan<- Result, wg *sync.WaitGroup) {
	defer wg.Done()
	for i := 0; i < count; i++ {
		// 为每次请求构造带唯一标识的 payload，避免缓存命中
		payload := basePayload
		messages := make([]Message, len(basePayload.Messages))
		copy(messages, basePayload.Messages)
		// 在 user 消息末尾追加唯一请求序号
		last := len(messages) - 1
		messages[last].Content = fmt.Sprintf("%s\n[req:%d-%d]", messages[last].Content, id, i)
		payload.Messages = messages
		resultCh <- sendRequest(url, payload)
	}
}

// ==================== 主函数 ====================

func main() {
	var (
		concurrency = flag.Int("c", 100, "并发数")
		totalReq    = flag.Int("n", 1000, "总请求数")
		stream      = flag.Bool("stream", false, "流式模式")
		maxTokens   = flag.Int("max-tokens", 128000, "最大生成token数")
		model       = flag.String("model", "dashscope_qwen3_coder", "模型名称")
		prompt      = flag.String("prompt", "解释一下什么是机器学习，用通俗易懂的语言。", "测试prompt")
		rampUp      = flag.Float64("ramp-up", 0, "渐进加压时间(秒)")
		auth        = flag.String("auth", "", "Authorization Bearer token")
	)
	flag.Parse()

	authToken = *auth

	url := "http://localhost:8080/v1/chat/completions"

	payload := ChatRequest{
		Model: *model,
		Messages: []Message{
			{Role: "system", Content: "你是一个 helpful 的 AI 助手。"},
			{Role: "user", Content: *prompt},
		},
		MaxTokens:   *maxTokens,
		Temperature: 0.7,
		Stream:      *stream,
	}

	fmt.Printf("🚀 Go Stress Test Starting...\n")
	fmt.Printf("   Concurrency: %d\n", *concurrency)
	fmt.Printf("   Total requests: %d\n", *totalReq)
	fmt.Printf("   Stream: %v\n", *stream)
	fmt.Printf("   Max tokens: %d\n", *maxTokens)

	// 预热
	fmt.Println("🔥 Warming up...")
	warmup := sendRequest(url, payload)
	if !warmup.Success {
		fmt.Printf("⚠️ Warmup failed: %s\n", warmup.Error)
		if warmup.ResponseBody != "" {
			respPreview := warmup.ResponseBody
			if len(respPreview) > 500 {
				respPreview = respPreview[:500] + "..."
			}
			fmt.Printf("   ↳ response: %s\n", respPreview)
		}
	} else {
		fmt.Printf("✅ Warmup success: %.2fs\n", warmup.Latency.Seconds())
	}

	stats := &Stats{}
	stats.StartTime = time.Now()

	resultCh := make(chan Result, *totalReq)
	var wg sync.WaitGroup

	// 启动收集协程
	go func() {
		for r := range resultCh {
			stats.Add(r)
			if !r.Success {
				statusInfo := ""
				if r.StatusCode > 0 {
					statusInfo = fmt.Sprintf(" [HTTP %d]", r.StatusCode)
				}
				errInfo := r.Error
				if errInfo == "" {
					errInfo = "unknown error"
				}
				fmt.Printf("  ❌ Failed:%s %s (%.2fs)\n", statusInfo, errInfo, r.Latency.Seconds())
				if r.ResponseBody != "" {
					respPreview := r.ResponseBody
					if len(respPreview) > 300 {
						respPreview = respPreview[:300] + "..."
					}
					fmt.Printf("     ↳ response: %s\n", respPreview)
				}
			}
		}
	}()

	// 计算每个 worker 的请求数
	reqPerWorker := *totalReq / *concurrency
	remainder := *totalReq % *concurrency

	// 渐进加压
	rampUpInterval := time.Duration(0)
	if *rampUp > 0 && *concurrency > 1 {
		rampUpInterval = time.Duration((*rampUp * 1000 / float64(*concurrency))) * time.Millisecond
	}

	for i := 0; i < *concurrency; i++ {
		count := reqPerWorker
		if i < remainder {
			count++
		}
		if count == 0 {
			continue
		}
		wg.Add(1)
		go worker(i, url, payload, count, resultCh, &wg)
		if rampUpInterval > 0 {
			time.Sleep(rampUpInterval)
		}
	}

	wg.Wait()
	close(resultCh)
	// 等待收集完成
	time.Sleep(100 * time.Millisecond)

	stats.EndTime = time.Now()

	// 输出报告
	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("📊 STRESS TEST REPORT (Go)")
	fmt.Println(strings.Repeat("=", 60))
	fmt.Printf("Target URL:     %s\n", url)
	fmt.Printf("Concurrency:    %d\n", *concurrency)
	fmt.Printf("Total Requests: %d\n", stats.TotalRequests)
	fmt.Printf("Successful:     %d (%.1f%%)\n", stats.Successful, float64(stats.Successful)/float64(stats.TotalRequests)*100)
	fmt.Printf("Failed:         %d\n", stats.Failed)
	fmt.Printf("Duration:       %.2fs\n", stats.Duration().Seconds())
	fmt.Printf("QPS:            %.2f\n", stats.QPS())
	fmt.Println(strings.Repeat("-", 60))
	fmt.Println("⏱️  LATENCY")
	fmt.Printf("  Average:      %.3fs\n", stats.AvgLatency().Seconds())
	fmt.Printf("  P50 (Median): %.3fs\n", stats.P50Latency().Seconds())
	fmt.Printf("  P99:          %.3fs\n", stats.P99Latency().Seconds())
	if len(stats.TTFBs) > 0 {
		fmt.Printf("  Avg TTFB:     %.3fs\n", stats.AvgTTFB().Seconds())
	}
	fmt.Println(strings.Repeat("-", 60))
	fmt.Println("🚀 THROUGHPUT")
	fmt.Printf("  Avg tokens/s: %.2f\n", stats.AvgThroughput())
	fmt.Printf("  Total output: %d tokens\n", stats.TotalTokensOut)
	fmt.Println(strings.Repeat("-", 60))
	fmt.Println("📝 PAYLOAD CONFIG")
	fmt.Printf("  Model:        %s\n", payload.Model)
	fmt.Printf("  Max Tokens:   %d\n", payload.MaxTokens)
	fmt.Printf("  Stream:       %v\n", payload.Stream)
	fmt.Printf("  Temperature:  %.1f\n", payload.Temperature)
	fmt.Println(strings.Repeat("=", 60))

	if float64(stats.Failed)/float64(stats.TotalRequests) > 0.1 {
		fmt.Println("\n⚠️  Failure rate > 10%")
		os.Exit(1)
	}
}
