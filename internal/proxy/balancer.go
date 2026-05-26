package proxy

import (
	"fmt"
	"sync"

	"github.com/rizxfrog/oh-my-api/internal/config"
)

type AccountPool struct {
	cfg config.AccountConfig
}

func NewAccountPool(cfg config.AccountConfig) *AccountPool {
	if cfg.RoutingMode == "" {
		cfg.RoutingMode = "mixed"
	}
	return &AccountPool{cfg: cfg}
}

func (p *AccountPool) Eligible(accounts []AccountSnapshot) []AccountSnapshot {
	eligible := make([]AccountSnapshot, 0, len(accounts))
	for _, account := range accounts {
		if !account.Enabled {
			continue
		}
		switch p.cfg.RoutingMode {
		case "china_only":
			if account.Region != AccountRegionChina {
				continue
			}
		case "international_only":
			if account.Region != AccountRegionInternational {
				continue
			}
		}
		eligible = append(eligible, account)
	}
	return eligible
}

type RoundRobinBalancer struct {
	mu    sync.Mutex
	index int
}

func NewRoundRobinBalancer() *RoundRobinBalancer {
	return &RoundRobinBalancer{}
}

func (b *RoundRobinBalancer) Next(accounts []AccountSnapshot) (AccountSnapshot, error) {
	if len(accounts) == 0 {
		return AccountSnapshot{}, fmt.Errorf("%w: empty account pool", ErrCredentialsUnavailable)
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	account := accounts[b.index%len(accounts)]
	b.index++
	return account, nil
}
