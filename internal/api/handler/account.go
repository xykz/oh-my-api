package handler

import (
	"context"
	"fmt"

	"github.com/rizxfrog/oh-my-api/internal/api/model"
	"github.com/rizxfrog/oh-my-api/internal/proxy"
)

func (s *Server) accountRoutingEnabled() bool {
	return s.Deps.Accounts != nil &&
		s.Deps.AccountPool != nil &&
		s.Deps.Balancer != nil &&
		s.Deps.Adapters != nil
}

func (s *Server) selectAccountAndAdapter(ctx context.Context, modelKey string) (proxy.AccountSnapshot, proxy.RegionAdapter, error) {
	accounts, err := s.Deps.Accounts.Accounts(ctx)
	if err != nil {
		return proxy.AccountSnapshot{}, nil, err
	}
	eligible := s.Deps.AccountPool.Eligible(accounts)
	if modelKey != "" {
		filtered, available, err := s.filterAccountsByModelCompatibility(ctx, eligible, modelKey)
		if err != nil {
			return proxy.AccountSnapshot{}, nil, err
		}
		if available {
			eligible = filtered
		}
	}
	account, err := s.Deps.Balancer.Next(eligible)
	if err != nil {
		return proxy.AccountSnapshot{}, nil, err
	}
	adapter, err := s.Deps.Adapters.ForRegion(account.Region)
	if err != nil {
		return proxy.AccountSnapshot{}, nil, err
	}
	return account, adapter, nil
}

func (s *Server) filterAccountsByModelCompatibility(ctx context.Context, accounts []proxy.AccountSnapshot, modelKey string) ([]proxy.AccountSnapshot, bool, error) {
	filtered, available, err := s.filterAccountsByModelAccounts(ctx, accounts, modelKey)
	if err != nil || available {
		return filtered, available, err
	}
	return s.filterAccountsByModelRegions(ctx, accounts, modelKey)
}

func (s *Server) filterAccountsByModelAccounts(ctx context.Context, accounts []proxy.AccountSnapshot, modelKey string) ([]proxy.AccountSnapshot, bool, error) {
	accountsProvider, ok := s.Deps.Models.(model.ModelAccountProvider)
	if !ok {
		return nil, false, nil
	}
	accountIDs, available, err := accountsProvider.AvailableAccounts(ctx, modelKey)
	if err != nil {
		return nil, true, err
	}
	if !available {
		return nil, false, nil
	}
	allowed := make(map[string]struct{}, len(accountIDs))
	for _, accountID := range accountIDs {
		allowed[accountID] = struct{}{}
	}
	filtered := make([]proxy.AccountSnapshot, 0, len(accounts))
	for _, account := range accounts {
		if _, ok := allowed[account.ID]; ok {
			filtered = append(filtered, account)
		}
	}
	if len(filtered) == 0 {
		return nil, true, fmt.Errorf("%w: no eligible accounts advertise model %q", proxy.ErrCredentialsUnavailable, modelKey)
	}
	return filtered, true, nil
}

func (s *Server) filterAccountsByModelRegions(ctx context.Context, accounts []proxy.AccountSnapshot, modelKey string) ([]proxy.AccountSnapshot, bool, error) {
	regionsProvider, ok := s.Deps.Models.(model.ModelRegionProvider)
	if !ok {
		return nil, false, nil
	}
	regions, available, err := regionsProvider.AvailableRegions(ctx, modelKey)
	if err != nil {
		return nil, true, err
	}
	if !available {
		return nil, false, nil
	}
	allowed := make(map[proxy.AccountRegion]struct{}, len(regions))
	for _, region := range regions {
		allowed[region] = struct{}{}
	}
	filtered := make([]proxy.AccountSnapshot, 0, len(accounts))
	for _, account := range accounts {
		if _, ok := allowed[account.Region]; ok {
			filtered = append(filtered, account)
		}
	}
	if len(filtered) == 0 {
		return nil, true, fmt.Errorf("%w: no eligible accounts advertise model %q", proxy.ErrCredentialsUnavailable, modelKey)
	}
	return filtered, true, nil
}
