package app

import "github.com/sainteye/clawdline/internal/contract"

// quotaWeights estimate how much of an assistant's plan allowance a token
// part consumes, relative to one uncached input token. Neither vendor
// publishes how a subscription limit is charged; these are the ratios of
// each vendor's published API prices for its current coding models, which
// is the closest public proxy. They are comparable only within one
// assistant: each assistant has its own allowance.
//
// On one machine's Codex rollouts for 2026-11-09, cache reads were 97% of a
// project's tokens but about two thirds of its weighted total, so ranking by
// raw tokens over-weights cache-heavy work against work that writes output.
var quotaWeights = map[string]contract.UsageOverviewQuotaWeight{
	// Anthropic: cache read 0.1x input, 5-minute cache write 1.25x, output 5x.
	"claude": {ID: "claude", Input: 1, CacheRead: 0.1, CacheWrite: 1.25, Output: 5},
	// OpenAI GPT-5 family: cached input 0.1x input, output 8x; no write charge.
	"codex": {ID: "codex", Input: 1, CacheRead: 0.1, CacheWrite: 1, Output: 8},
	// Google Gemini Pro: cached input 0.25x input, output 8x.
	"gemini": {ID: "gemini", Input: 1, CacheRead: 0.25, CacheWrite: 1, Output: 8},
}

// defaultQuotaWeight is used for an assistant without published ratios.
var defaultQuotaWeight = contract.UsageOverviewQuotaWeight{ID: "default", Input: 1, CacheRead: 0.1, CacheWrite: 1.25, Output: 5}

func quotaWeightOf(agent string) contract.UsageOverviewQuotaWeight {
	if w, ok := quotaWeights[agent]; ok {
		return w
	}
	return defaultQuotaWeight
}

func weightedTokens(agent string, input, cacheRead, cacheWrite, output float64) float64 {
	w := quotaWeightOf(agent)
	return input*w.Input + cacheRead*w.CacheRead + cacheWrite*w.CacheWrite + output*w.Output
}
