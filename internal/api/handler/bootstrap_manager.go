package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/rizxfrog/oh-my-api/internal/auth"
	"github.com/rizxfrog/oh-my-api/internal/config"
	"github.com/rizxfrog/oh-my-api/internal/proxy"
)

type BootstrapSession struct {
	ID         string                   `json:"id"`
	Status     string                   `json:"status"`
	Method     string                   `json:"method"`
	Phase      string                   `json:"phase,omitempty"`
	AuthURL    string                   `json:"auth_url,omitempty"`
	Error      string                   `json:"error,omitempty"`
	StartedAt  time.Time                `json:"started_at"`
	ExpiresAt  time.Time                `json:"expires_at,omitempty"`
	cancel     context.CancelFunc       `json:"-"`
	machineID  string                   `json:"-"`
	state      string                   `json:"-"`
	verifier   string                   `json:"-"`
	nonce      string                   `json:"-"`
	lingmaRPC  *auth.LingmaLoginSession `json:"-"`
	listenAddr string                   `json:"-"`
	cacheMTime time.Time                `json:"-"`
}

type BootstrapManager struct {
	mu                 sync.Mutex
	sessions           map[string]*BootstrapSession
	authFile           string
	listenAddr         string
	lingmaVer          string
	AutoDetectFreePort bool
	Accounts           BootstrapAccountStore

	OnCredentialSaved func()
}

type BootstrapAccountStore interface {
	UpsertAccount(context.Context, proxy.StoredCredentialAccount) error
}

func NewBootstrapManager(authFile, listenAddr, lingmaVer string) *BootstrapManager {
	if listenAddr == "" {
		listenAddr = "127.0.0.1:37510"
	}
	return &BootstrapManager{
		sessions:   make(map[string]*BootstrapSession),
		authFile:   authFile,
		listenAddr: listenAddr,
		lingmaVer:  lingmaVer,
		Accounts:   proxy.NewAccountStore(config.CredentialConfig{AuthFile: authFile}, nil),
	}
}

func (m *BootstrapManager) AuthFile() string {
	return m.authFile
}

func (m *BootstrapManager) GetStatus(id string) *BootstrapSession {
	m.mu.Lock()
	defer m.mu.Unlock()

	sess, ok := m.sessions[id]
	if !ok {
		return nil
	}

	if time.Since(sess.StartedAt) > 10*time.Minute {
		delete(m.sessions, id)
	}

	snapshot := *sess
	snapshot.cancel = nil
	return &snapshot
}

func (m *BootstrapManager) updateSession(id, status, errMsg string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[id]; ok {
		s.Status = status
		s.Error = errMsg
	}
}

func (m *BootstrapManager) updateSessionWithPhase(id, status, phase, errMsg string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[id]; ok {
		s.Status = status
		s.Phase = phase
		s.Error = errMsg
	}
}

func (m *BootstrapManager) findActiveLocked() *BootstrapSession {
	for _, s := range m.sessions {
		switch s.Status {
		case "running", "awaiting_callback_url", "deriving":
			return s
		}
	}
	return nil
}

func (m *BootstrapManager) Start(method string) (*BootstrapSession, error) {
	switch method {
	case "", "remote_callback":
		return m.StartRemoteCallback()
	default:
		return nil, fmt.Errorf("invalid method: %s", method)
	}
}

func (m *BootstrapManager) Cancel(id string) error {
	m.mu.Lock()
	sess, ok := m.sessions[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("session not found")
	}
	switch sess.Status {
	case "running", "awaiting_callback_url", "deriving":
	default:
		m.mu.Unlock()
		return fmt.Errorf("session already %s", sess.Status)
	}
	cancel := sess.cancel
	closeLingmaLoginSession(sess)
	sess.cancel = nil
	sess.Status = "cancelled"
	sess.Error = ""
	sess.Phase = ""
	m.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	return nil
}

func (m *BootstrapManager) logAndReload(id, uid, aid, name, cosyKey, machineID, expireTime string) {
	fmt.Printf("[bootstrap] OAuth login successful (session=%s)\n", id)
	fmt.Printf("[bootstrap]   uid:        %s\n", uid)
	fmt.Printf("[bootstrap]   aid:        %s\n", aid)
	fmt.Printf("[bootstrap]   name:       %s\n", name)
	fmt.Printf("[bootstrap]   machine_id: %s\n", machineID)
	if len(cosyKey) > 20 {
		fmt.Printf("[bootstrap]   cosy_key:   %s...\n", cosyKey[:20])
	} else if cosyKey != "" {
		fmt.Printf("[bootstrap]   cosy_key:   %s\n", cosyKey)
	}
	fmt.Printf("[bootstrap]   expire:     %s\n", expireTime)

	m.mu.Lock()
	if sess, ok := m.sessions[id]; ok {
		closeLingmaLoginSession(sess)
		sess.Status = "completed"
		sess.Error = ""
	}
	m.mu.Unlock()

	if m.OnCredentialSaved != nil {
		m.OnCredentialSaved()
	}
}

func closeLingmaLoginSession(sess *BootstrapSession) {
	if sess == nil || sess.lingmaRPC == nil {
		return
	}
	sess.lingmaRPC.Close()
	sess.lingmaRPC = nil
}

func newSessionID() string {
	var buf [8]byte
	_, _ = rand.Read(buf[:])
	return hex.EncodeToString(buf[:])
}
