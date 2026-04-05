package global

import (
	"context"
	"time"

	"fiatjaf.com/nostr"
	"fiatjaf.com/nostr/khatru/policies"
)

var GroupCreateRateLimiter func(ctx context.Context, event nostr.Event) (reject bool, msg string)

func ConfigureGroupCreateRateLimit(settings Settings) {
	cfg := settings.Groups.CreateGroupRateLimit
	if cfg.TokensPerInterval <= 0 || cfg.IntervalSeconds <= 0 || cfg.MaxTokens <= 0 {
		GroupCreateRateLimiter = nil
		return
	}

	GroupCreateRateLimiter = policies.EventIPRateLimiter(
		cfg.TokensPerInterval,
		time.Duration(cfg.IntervalSeconds)*time.Second,
		cfg.MaxTokens,
	)
}
