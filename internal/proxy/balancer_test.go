package proxy

import (
	"errors"
	"reflect"
	"testing"

	"github.com/rizxfrog/oh-my-api/internal/config"
)

func TestAccountPoolFiltersByRoutingMode(t *testing.T) {
	accounts := []AccountSnapshot{
		{ID: "china-1", Region: AccountRegionChina, Enabled: true},
		{ID: "intl-1", Region: AccountRegionInternational, Enabled: true},
		{ID: "china-disabled", Region: AccountRegionChina, Enabled: false},
	}

	tests := []struct {
		name        string
		routingMode string
		want        []string
	}{
		{
			name:        "china only",
			routingMode: "china_only",
			want:        []string{"china-1"},
		},
		{
			name:        "international only",
			routingMode: "international_only",
			want:        []string{"intl-1"},
		},
		{
			name:        "mixed",
			routingMode: "mixed",
			want:        []string{"china-1", "intl-1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pool := NewAccountPool(config.AccountConfig{RoutingMode: tt.routingMode})

			got := idsOf(pool.Eligible(accounts))

			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Eligible ids = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRoundRobinBalancerIsAccountAverage(t *testing.T) {
	accounts := []AccountSnapshot{
		{ID: "china-1", Region: AccountRegionChina, Enabled: true},
		{ID: "china-2", Region: AccountRegionChina, Enabled: true},
		{ID: "intl-1", Region: AccountRegionInternational, Enabled: true},
	}
	balancer := NewRoundRobinBalancer()

	var got []string
	for i := 0; i < 5; i++ {
		account, err := balancer.Next(accounts)
		if err != nil {
			t.Fatalf("Next() error = %v", err)
		}
		got = append(got, account.ID)
	}

	want := []string{"china-1", "china-2", "intl-1", "china-1", "china-2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Next ids = %v, want %v", got, want)
	}
}

func TestRoundRobinBalancerRejectsEmptyPool(t *testing.T) {
	balancer := NewRoundRobinBalancer()

	_, err := balancer.Next(nil)

	if !errors.Is(err, ErrCredentialsUnavailable) {
		t.Fatalf("Next(nil) error = %v, want ErrCredentialsUnavailable", err)
	}
}

func idsOf(accounts []AccountSnapshot) []string {
	ids := make([]string, len(accounts))
	for i, account := range accounts {
		ids[i] = account.ID
	}
	return ids
}
