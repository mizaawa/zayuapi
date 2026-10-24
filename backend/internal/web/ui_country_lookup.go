//go:build embed

package web

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/pkg/uiaccess"
)

type uiCountryResolver interface {
	Country(context.Context, string, bool) (string, error)
}

type sharedUICountryResolver struct{}

func newUICountryResolver() sharedUICountryResolver { return sharedUICountryResolver{} }

func (sharedUICountryResolver) Country(ctx context.Context, ip string, refresh bool) (string, error) {
	result, err := uiaccess.DetectCountry(ctx, ip, refresh)
	return result.Country, err
}
