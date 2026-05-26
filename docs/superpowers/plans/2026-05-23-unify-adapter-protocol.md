# 统一适配器基协议 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将国内版和国际版共享的 Lingma 协议实现提取为 `LingmaAdapter` 基类，两者仅差 `baseURL` 和 `Region()`，消除 `InternationalAdapter` 的空壳实现。

**Architecture:** 引入 `LingmaAdapter` 结构体，内嵌 `NativeTransport` + `BodyBuilder` + `region` + `now`，实现完整的 `RegionAdapter` 接口。`ChinaAdapter` 和 `InternationalAdapter` 变为类型别名或薄包装，只提供各自默认的 `baseURL` 和 `Region()`。`main.go` 的创建逻辑简化：两个 adapter 共用同一个 `SignatureEngine` 和 `BodyBuilder`，仅 `NativeTransport` 的 `baseURL` 不同。

**Tech Stack:** Go 1.x, existing proxy package

---

### Task 1: 创建 `LingmaAdapter` 基协议结构体

**Files:**
- Create: `internal/proxy/lingma_adapter.go`

- [ ] **Step 1: 创建 `lingma_adapter.go`，定义 `LingmaAdapter` 及其构造函数**

```go
package proxy

import (
	"context"
	"fmt"
	"io"
	"time"
)

// LingmaAdapter implements the RegionAdapter interface for the Lingma protocol.
// Both China and International regions use the same protocol, differing only in
// baseURL and region identifier.
type LingmaAdapter struct {
	region    AccountRegion
	transport *NativeTransport
	builder   *BodyBuilder
	now       func() time.Time
}

// NewLingmaAdapter creates a LingmaAdapter for the given region with the
// provided transport and body builder.
func NewLingmaAdapter(region AccountRegion, transport *NativeTransport, builder *BodyBuilder, now func() time.Time) (*LingmaAdapter, error) {
	if transport == nil {
		return nil, ErrLingmaAdapterNilTransport
	}
	if builder == nil {
		return nil, ErrLingmaAdapterNilBuilder
	}
	if now == nil {
		now = time.Now
	}
	return &LingmaAdapter{
		region:    region,
		transport: transport,
		builder:   builder,
		now:       now,
	}, nil
}

func (a *LingmaAdapter) Region() AccountRegion {
	return a.region
}

func (a *LingmaAdapter) ListModels(ctx context.Context, account AccountSnapshot) ([]RemoteModel, error) {
	return a.transport.ListModels(ctx, account.ToCredentialSnapshot())
}

func (a *LingmaAdapter) BuildChatRequest(_ context.Context, canonical CanonicalRequest, modelKey string, _ AccountSnapshot) (RemoteChatRequest, error) {
	return a.builder.BuildCanonical(canonical, modelKey)
}

func (a *LingmaAdapter) StreamChat(ctx context.Context, request RemoteChatRequest, account AccountSnapshot) (io.ReadCloser, error) {
	return a.transport.StreamChat(ctx, request, account.ToCredentialSnapshot())
}

func (a *LingmaAdapter) UploadImage(ctx context.Context, account AccountSnapshot, imageURI string) (string, error) {
	return a.transport.UploadImage(ctx, account.ToCredentialSnapshot(), imageURI)
}

func (a *LingmaAdapter) TestConnection(ctx context.Context, account AccountSnapshot) AccountTestResult {
	models, err := a.ListModels(ctx, account)
	if err != nil {
		return AccountTestResult{
			AccountID:    account.ID,
			AccountLabel: account.Label,
			Region:       account.Region,
			Success:      false,
			Error:        err.Error(),
			Timestamp:    a.now().Format(time.RFC3339),
		}
	}

	return AccountTestResult{
		AccountID:       account.ID,
		AccountLabel:    account.Label,
		Region:          account.Region,
		Success:         true,
		StatusCode:      200,
		ResponsePreview: fmt.Sprintf("ListModels returned %d models", len(models)),
		Timestamp:       a.now().Format(time.RFC3339),
	}
}
```

---

### Task 2: 添加 `LingmaAdapter` 的错误哨兵值

**Files:**
- Modify: `internal/proxy/adapters.go`

- [ ] **Step 1: 在 `adapters.go` 中添加 `LingmaAdapter` 专属的错误变量**

在现有 `ErrAdapterProtocolNotConfigured` 和 `ErrAdapterNil` 之后添加：

```go
var ErrLingmaAdapterNilTransport = errors.New("lingma adapter native transport is nil")
var ErrLingmaAdapterNilBuilder = errors.New("lingma adapter body builder is nil")
```

---

### Task 3: 重写 `ChinaAdapter` 为 `LingmaAdapter` 的薄包装

**Files:**
- Modify: `internal/proxy/china_adapter.go`

- [ ] **Step 1: 将 `ChinaAdapter` 改为对 `LingmaAdapter` 的类型别名 + 便捷构造函数**

`ChinaAdapter` 的错误哨兵 `ErrChinaAdapterNilTransport` / `ErrChinaAdapterNilBuilder` 保留为对新的 `ErrLingmaAdapter*` 的别名，保持向后兼容。

```go
package proxy

import "time"

// ErrChinaAdapterNilTransport is an alias for ErrLingmaAdapterNilTransport.
//
// Deprecated: Use ErrLingmaAdapterNilTransport instead.
var ErrChinaAdapterNilTransport = ErrLingmaAdapterNilTransport

// ErrChinaAdapterNilBuilder is an alias for ErrLingmaAdapterNilBuilder.
//
// Deprecated: Use ErrLingmaAdapterNilBuilder instead.
var ErrChinaAdapterNilBuilder = ErrLingmaAdapterNilBuilder

// ChinaAdapter is a LingmaAdapter pre-configured for the China region.
type ChinaAdapter = LingmaAdapter

// NewChinaAdapter creates a LingmaAdapter for the China region.
func NewChinaAdapter(transport *NativeTransport, builder *BodyBuilder, now func() time.Time) (*ChinaAdapter, error) {
	return NewLingmaAdapter(AccountRegionChina, transport, builder, now)
}
```

---

### Task 4: 重写 `InternationalAdapter` 为 `LingmaAdapter` 的薄包装

**Files:**
- Modify: `internal/proxy/international_adapter.go`

- [ ] **Step 1: 将 `InternationalAdapter` 改为对 `LingmaAdapter` 的类型别名 + 便捷构造函数**

构造函数接收 `baseURL`，内部创建 `NativeTransport`（用同一个 signer）和 `BodyBuilder`。

```go
package proxy

import "time"

// InternationalAdapter is a LingmaAdapter pre-configured for the International region.
type InternationalAdapter = LingmaAdapter

// NewInternationalAdapter creates a LingmaAdapter for the International region.
// It accepts a signer parameter (same SignatureEngine as China) because the
// protocol is identical — only the baseURL differs.
func NewInternationalAdapter(baseURL string, signerAndBuilder ...interface{}) *InternationalAdapter {
	// Backward-compatible: if called with just baseURL, return a stub that will
	// fail at runtime (matching old behavior). The full constructor is
	// NewInternationalAdapterWithProtocol.
	if len(signerAndBuilder) == 0 {
		// Return nil; callers that only pass baseURL get the old behavior
		// where the adapter is not fully functional.
		// This preserves backward compatibility with test code.
		return nil
	}
	// This signature is not ideal — see NewInternationalAdapterFull below.
	return nil
}
```

实际上，可变参数签名不优雅。更好的做法是提供 `NewInternationalAdapterFull` 函数，并让 `main.go` 调用它。但为了更简洁，直接改 `NewInternationalAdapter` 签名。

**最终方案**：直接修改 `NewInternationalAdapter` 签名，加上必要参数，所有调用点一起更新。

```go
package proxy

import (
	"strings"
	"time"
)

// InternationalAdapter is a LingmaAdapter pre-configured for the International region.
type InternationalAdapter = LingmaAdapter

// NewInternationalAdapter creates a LingmaAdapter for the International region.
// baseURL defaults to "https://api.lingma.ai" if empty.
func NewInternationalAdapter(baseURL string, transport *NativeTransport, builder *BodyBuilder, now func() time.Time) (*InternationalAdapter, error) {
	if baseURL == "" {
		baseURL = "https://api.lingma.ai"
	}
	if transport == nil {
		// Create a new transport with the international baseURL
		transport = NewNativeTransport(strings.TrimRight(baseURL, "/"), nil, 90*time.Second)
	} else {
		// Override the transport's baseURL if needed — but NativeTransport
		// doesn't expose a setter. The transport should already be created
		// with the correct baseURL.
		_ = baseURL
	}
	return NewLingmaAdapter(AccountRegionInternational, transport, builder, now)
}
```

但这里有个问题：`NativeTransport` 的 `baseURL` 在创建时就确定了，不能事后修改。所以正确的做法是：**调用方负责用正确的 baseURL 创建 `NativeTransport`**，`NewInternationalAdapter` 不再自己管 baseURL。

**更简洁的最终方案：**

```go
package proxy

import "time"

// InternationalAdapter is a LingmaAdapter pre-configured for the International region.
type InternationalAdapter = LingmaAdapter

// NewInternationalAdapter creates a LingmaAdapter for the International region.
// The caller must provide a NativeTransport configured with the correct international baseURL.
func NewInternationalAdapter(transport *NativeTransport, builder *BodyBuilder, now func() time.Time) (*InternationalAdapter, error) {
	return NewLingmaAdapter(AccountRegionInternational, transport, builder, now)
}

// NewInternationalAdapterStub creates a non-functional InternationalAdapter
// that returns ErrAdapterProtocolNotConfigured for all operations.
// This preserves backward compatibility with code that only knows the baseURL.
//
// Deprecated: Use NewInternationalAdapter with a proper transport instead.
func NewInternationalAdapterStub(baseURL string) *InternationalAdapter {
	// Return nil — ForRegion will handle the nil case, or tests can be updated.
	// Actually, we can't return nil as a *LingmaAdapter that satisfies RegionAdapter
	// because callers will nil-check. Let's just not provide this.
	_ = baseURL
	return nil
}
```

实际上最干净的方案：**直接改签名，不保留旧接口**。因为旧的 `NewInternationalAdapter(baseURL)` 返回的是空壳，实际外部调用者只有 `main.go` 和测试。

---

### Task 5: 更新 `main.go` 中的适配器创建逻辑

**Files:**
- Modify: `main.go`

- [ ] **Step 1: 更新 `main.go`，为国际版创建独立的 `NativeTransport`**

当前 `main.go:45-57`：

```go
chinaTransport := proxy.NewNativeTransport(firstNonEmpty(cfg.Account.ChinaBaseURL, cfg.Lingma.BaseURL), chinaSigner, 90*time.Second)
// ...
chinaAdapter, err := proxy.NewChinaAdapter(chinaTransport, builder, time.Now)
// ...
adapters.Register(proxy.NewInternationalAdapter(cfg.Account.InternationalBaseURL))
```

改为：

```go
chinaTransport := proxy.NewNativeTransport(firstNonEmpty(cfg.Account.ChinaBaseURL, cfg.Lingma.BaseURL), chinaSigner, 90*time.Second)
intlTransport := proxy.NewNativeTransport(cfg.Account.InternationalBaseURL, chinaSigner, 90*time.Second)
// ...
chinaAdapter, err := proxy.NewChinaAdapter(chinaTransport, builder, time.Now)
// ...
intlAdapter, err := proxy.NewInternationalAdapter(intlTransport, builder, time.Now)
if err != nil {
    log.Fatalf("create international adapter: %v", err)
}
if err := adapters.Register(intlAdapter); err != nil {
    log.Fatalf("register international adapter: %v", err)
}
```

这样国内版和国际版各自有独立的 `NativeTransport`（不同的 `baseURL`），但共享同一个 `SignatureEngine` 和 `BodyBuilder`。

---

### Task 6: 更新测试

**Files:**
- Modify: `internal/proxy/adapters_test.go`
- Modify: `internal/proxy/models_test.go`
- Modify: `internal/api/account_routing_test.go`

- [ ] **Step 1: 更新 `adapters_test.go` 中所有 `NewInternationalAdapter` 调用**

旧签名：`NewInternationalAdapter("https://api.lingma.ai")`
新签名：`NewInternationalAdapter(transport, builder, now)`

需要创建对应的 `NativeTransport` 和 `BodyBuilder`。

Test `TestAdapterRegistryReturnsAdapterByRegion`:
```go
func TestAdapterRegistryReturnsAdapterByRegion(t *testing.T) {
	registry := NewAdapterRegistry()
	transport := NewNativeTransport("https://api.lingma.ai", nil, 0)
	builder := NewBodyBuilder("", nil, nil, nil)
	adapter, err := NewInternationalAdapter(transport, builder, nil)
	if err != nil {
		t.Fatalf("NewInternationalAdapter() error = %v", err)
	}
	if err := registry.Register(adapter); err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	// ... rest unchanged
}
```

Test `TestAdapterRegistryRegisterRejectsNilAdapter`:
```go
// Keep the nil-interface test as-is
err := registry.Register(nil)
// ... 

// For typed nil test, use (*LingmaAdapter)(nil)
var adapter *LingmaAdapter
err = registry.Register(adapter)
```

Test `TestAdapterRegistryZeroValueIsSafe`:
```go
// Similar to TestAdapterRegistryReturnsAdapterByRegion
transport := NewNativeTransport("https://api.lingma.ai", nil, 0)
builder := NewBodyBuilder("", nil, nil, nil)
adapter, err := NewInternationalAdapter(transport, builder, nil)
```

Test `TestInternationalAdapterReportsProtocolNotConfigured`:
删除此测试，因为 `InternationalAdapter` 不再返回 `ErrAdapterProtocolNotConfigured`。替换为验证国际版适配器可以正常调用（或用 nil signer 测试请求会失败）。

- [ ] **Step 2: 更新 `TestNewChinaAdapterRequiresTransport` 和 `TestNewChinaAdapterRequiresBodyBuilder`**

这些测试现在应该使用 `ErrLingmaAdapterNilTransport` 和 `ErrLingmaAdapterNilBuilder`，但因为 `ErrChinaAdapterNilTransport` 是别名，旧断言仍然通过。添加新测试直接验证 `NewLingmaAdapter`：

```go
func TestNewLingmaAdapterRequiresTransport(t *testing.T) {
	_, err := NewLingmaAdapter(AccountRegionChina, nil, NewBodyBuilder("", nil, nil, nil), nil)
	if !errors.Is(err, ErrLingmaAdapterNilTransport) {
		t.Fatalf("error = %v, want ErrLingmaAdapterNilTransport", err)
	}
}

func TestNewLingmaAdapterRequiresBuilder(t *testing.T) {
	transport := NewNativeTransport("https://example.com", nil, 0)
	_, err := NewLingmaAdapter(AccountRegionInternational, transport, nil, nil)
	if !errors.Is(err, ErrLingmaAdapterNilBuilder) {
		t.Fatalf("error = %v, want ErrLingmaAdapterNilBuilder", err)
	}
}
```

- [ ] **Step 3: 检查 `account_routing_test.go` 中的 `recordingAdapter` 等测试辅助**

搜索 `account_routing_test.go` 中是否有对 `InternationalAdapter` 的直接引用，如果有则更新。

- [ ] **Step 4: 运行全部测试确认通过**

Run: `go test ./internal/proxy/... ./internal/api/... -v`
Expected: All tests PASS

---

### Task 7: 清理废弃代码

**Files:**
- Modify: `internal/proxy/international_adapter.go`

- [ ] **Step 1: 删除 `internationalProtocolNotConfiguredError` 函数**

该函数不再需要，因为 `InternationalAdapter` 现在是 `LingmaAdapter` 的别名，所有方法都有真实实现。

- [ ] **Step 2: 确认 `ErrAdapterProtocolNotConfigured` 仍在 `adapters.go` 中保留**

`ErrAdapterProtocolNotConfigured` 仍被 `AdapterRegistry.ForRegion` 使用（当没有注册某个 region 的 adapter 时返回），不应删除。

- [ ] **Step 3: 运行全部测试**

Run: `go test ./...`
Expected: All tests PASS

---

### Task 8: 端到端验证

- [ ] **Step 1: 启动服务并验证国内版请求正常**

Run: `go run . -config ./config.yaml`
Test: 发送 `/v1/chat/completions` 请求，确认国内版正常工作。

- [ ] **Step 2: 验证国际版请求正常（如有国际版账号）**

如果配置了国际版账号，切换 `routing_mode` 到 `international_only` 测试。

- [ ] **Step 3: Commit**

```bash
git add -A
git commit -m "refactor: unify China/International adapter into LingmaAdapter base protocol"
```
