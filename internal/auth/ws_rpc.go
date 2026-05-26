package auth

import (
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/gorilla/websocket"
)

type LingmaLoginURLResult struct {
	LoginURL        string `json:"loginUrl"`
	Nonce           string `json:"nonce"`
	Verifier        string `json:"verifier"`
	Challenge       string `json:"challenge"`
	ChallengeMethod string `json:"challengeMethod"`
	ErrorCode       string `json:"errorCode"`
	ErrorMsg        string `json:"errorMsg"`
	Success         bool   `json:"success"`
}

type LingmaLoginCallbackResult struct {
	Success   bool   `json:"success"`
	ErrorCode string `json:"errorCode"`
	ErrorMsg  string `json:"errorMsg"`
}

type LingmaLoginSession struct {
	conn       *websocket.Conn
	socketPort int
	timeout    time.Duration
}

func lingmaLoginAuthCallbackMethod() string {
	return "login/auth_callback"
}

func lingmaLoginGenerateURLMethod() string {
	return "login/generateUrl"
}

func GenerateLingmaLoginURLViaWebSocket(socketPort int, timeout time.Duration) (LingmaLoginURLResult, error) {
	session, err := StartLingmaLoginSession(socketPort, timeout)
	if err != nil {
		return LingmaLoginURLResult{}, err
	}
	defer session.Close()
	return session.GenerateURL()
}

func StartLingmaLoginSession(socketPort int, timeout time.Duration) (*LingmaLoginSession, error) {
	if socketPort == 0 {
		socketPort = 37010
	}
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	conn, err := openInitializedLingmaWebSocket(socketPort, timeout, "login/session")
	if err != nil {
		return nil, err
	}
	return &LingmaLoginSession{conn: conn, socketPort: socketPort, timeout: timeout}, nil
}

func (s *LingmaLoginSession) Close() {
	if s == nil || s.conn == nil {
		return
	}
	_ = s.conn.Close()
	s.conn = nil
}

func (s *LingmaLoginSession) GenerateURL() (LingmaLoginURLResult, error) {
	var result LingmaLoginURLResult
	if err := s.call(lingmaLoginGenerateURLMethod(), map[string]any{}, &result); err != nil {
		return LingmaLoginURLResult{}, err
	}
	log.Printf("[callback-debug] Lingma login/generateUrl result success=%t error_code=%q login_url_len=%d nonce_len=%d verifier_len=%d",
		result.Success, result.ErrorCode, len(result.LoginURL), len(result.Nonce), len(result.Verifier))
	if !result.Success {
		return result, fmt.Errorf("lingma generateUrl: %s (%s)", result.ErrorMsg, result.ErrorCode)
	}
	return result, nil
}

func SubmitLingmaLoginCallbackViaWebSocket(socketPort int, timeout time.Duration, nonce, authParam, tokenString string) (LingmaLoginCallbackResult, error) {
	session, err := StartLingmaLoginSession(socketPort, timeout)
	if err != nil {
		return LingmaLoginCallbackResult{}, err
	}
	defer session.Close()
	return session.SubmitCallback(nonce, authParam, tokenString)
}

func (s *LingmaLoginSession) SubmitCallback(nonce, authParam, tokenString string) (LingmaLoginCallbackResult, error) {
	var result LingmaLoginCallbackResult
	if nonce == "" {
		return result, fmt.Errorf("missing nonce")
	}
	if authParam == "" {
		return result, fmt.Errorf("missing auth")
	}
	if tokenString == "" {
		return result, fmt.Errorf("missing tokenString")
	}
	params := map[string]any{
		"nonce":       nonce,
		"auth":        authParam,
		"tokenString": tokenString,
	}
	if err := s.call(lingmaLoginAuthCallbackMethod(), params, &result); err != nil {
		return LingmaLoginCallbackResult{}, err
	}
	log.Printf("[callback-debug] Lingma login/auth_callback result success=%t error_code=%q error_msg_len=%d",
		result.Success, result.ErrorCode, len(result.ErrorMsg))
	if !result.Success {
		return result, fmt.Errorf("lingma auth_callback: %s (%s)", result.ErrorMsg, result.ErrorCode)
	}
	return result, nil
}

func (s *LingmaLoginSession) call(method string, params any, out any) error {
	if s == nil || s.conn == nil {
		return fmt.Errorf("lingma login session is closed")
	}
	s.conn.SetReadDeadline(time.Now().Add(s.timeout))
	if err := wsSendMethod(s.conn, method, params, 2); err != nil {
		log.Printf("[callback-debug] Lingma ws rpc send method failed method=%s err=%v", method, err)
		return fmt.Errorf("send %s: %w", method, err)
	}
	log.Printf("[callback-debug] Lingma ws rpc method sent method=%s", method)
	if err := waitLingmaRPCResponse(s.conn, 2, out); err != nil {
		log.Printf("[callback-debug] Lingma ws rpc method response failed method=%s err=%v", method, err)
		return fmt.Errorf("read %s response: %w", method, err)
	}
	log.Printf("[callback-debug] Lingma ws rpc method response ok method=%s", method)
	return nil
}

func callLingmaWebSocketRPC(socketPort int, timeout time.Duration, method string, params any, out any) error {
	session, err := StartLingmaLoginSession(socketPort, timeout)
	if err != nil {
		return err
	}
	defer session.Close()
	return session.call(method, params, out)
}

func openInitializedLingmaWebSocket(socketPort int, timeout time.Duration, method string) (*websocket.Conn, error) {
	if socketPort == 0 {
		socketPort = 37010
	}
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	wsURL := fmt.Sprintf("ws://127.0.0.1:%d", socketPort)
	log.Printf("[callback-debug] Lingma ws rpc connect port=%d method=%s timeout_ms=%d",
		socketPort, method, timeout.Milliseconds())
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		log.Printf("[callback-debug] Lingma ws rpc connect failed port=%d method=%s err=%v", socketPort, method, err)
		return nil, fmt.Errorf("connect to lingma ws: %w", err)
	}
	conn.SetReadDeadline(time.Now().Add(timeout))

	if err := wsSendMethod(conn, "initialize", map[string]any{
		"processId":    nil,
		"clientInfo":   map[string]string{"name": "lingma2api-login", "version": "1.0"},
		"rootUri":      "file:///tmp/lingma2api-login",
		"capabilities": map[string]any{},
		"workspaceFolders": []map[string]string{
			{"uri": "file:///tmp/lingma2api-login", "name": "lingma2api-login"},
		},
	}, 1); err != nil {
		log.Printf("[callback-debug] Lingma ws rpc send initialize failed method=%s err=%v", method, err)
		_ = conn.Close()
		return nil, fmt.Errorf("ws initialize: %w", err)
	}
	log.Printf("[callback-debug] Lingma ws rpc initialize sent method=%s", method)

	if err := waitLingmaRPCResponse(conn, 1, nil); err != nil {
		log.Printf("[callback-debug] Lingma ws rpc initialize response failed method=%s err=%v", method, err)
		_ = conn.Close()
		return nil, fmt.Errorf("read init response: %w", err)
	}
	log.Printf("[callback-debug] Lingma ws rpc initialize response ok method=%s", method)

	initNotif := `{"jsonrpc":"2.0","method":"initialized","params":{}}`
	if err := conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(initNotif), initNotif))); err != nil {
		log.Printf("[callback-debug] Lingma ws rpc send initialized failed method=%s err=%v", method, err)
		_ = conn.Close()
		return nil, fmt.Errorf("send initialized: %w", err)
	}

	return conn, nil
}

func waitLingmaRPCResponse(conn *websocket.Conn, id int, out any) error {
	for {
		raw, err := wsReadMessage(conn)
		if err != nil {
			return err
		}
		var msg struct {
			ID     int             `json:"id"`
			Method string          `json:"method"`
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		if err := json.Unmarshal(raw, &msg); err != nil {
			log.Printf("[callback-debug] Lingma ws rpc frame ignored target_id=%d raw_len=%d unmarshal_err=%v",
				id, len(raw), err)
			continue
		}
		log.Printf("[callback-debug] Lingma ws rpc frame target_id=%d frame_id=%d method=%q result_len=%d error_len=%d",
			id, msg.ID, msg.Method, len(msg.Result), len(msg.Error))
		if msg.ID != id {
			continue
		}
		if len(msg.Error) > 0 && string(msg.Error) != "null" {
			return fmt.Errorf("rpc error: %s", string(msg.Error))
		}
		if out != nil && len(msg.Result) > 0 {
			if err := json.Unmarshal(msg.Result, out); err != nil {
				return fmt.Errorf("decode result: %w", err)
			}
		}
		return nil
	}
}
