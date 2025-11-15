package handlers

import "time"

// Request timeouts and limits
const (
	// DefaultRequestTimeout is the timeout for HTTP requests to the collector service
	DefaultRequestTimeout = 10 * time.Second

	// DefaultArticleLimit is the maximum number of articles to fetch
	DefaultArticleLimit = 50

	// DefaultArticleOffset is the starting offset for article pagination
	DefaultArticleOffset = 0
)

// Reddit source configuration defaults and constraints
const (
	// Reddit post limits
	RedditDefaultLimit = 25
	RedditMinLimit     = 1
	RedditMaxLimit     = 100

	// Reddit score filtering
	RedditDefaultMinScore = 10
	RedditMinMinScore     = 0
	RedditMaxMinScore     = 10000

	// Reddit comments filtering
	RedditDefaultMinComments = 5
	RedditMinMinComments     = 0
	RedditMaxMinComments     = 1000

	// Reddit rate limiting (milliseconds)
	RedditDefaultRateLimitDelay = 2000
	RedditMinRateLimitDelay     = 100
	RedditMaxRateLimitDelay     = 60000
)

// Semantic Scholar source configuration defaults and constraints
const (
	// Semantic Scholar result limits
	S2DefaultMaxResults = 25
	S2MinMaxResults     = 1
	S2MaxMaxResults     = 100

	// Semantic Scholar citation filtering
	S2DefaultMinCitations = 10
	S2MinMinCitations     = 0
	S2MaxMinCitations     = 100000

	// Semantic Scholar rate limiting (milliseconds)
	S2DefaultRateLimitDelay = 1000
	S2MinRateLimitDelay     = 100
	S2MaxRateLimitDelay     = 60000
)
