package models

// Source types
const (
	SourceTypeReddit          = "reddit"
	SourceTypeSemanticScholar = "semantic_scholar"
	SourceTypeHackerNews      = "hackernews"
)

// Reddit sort options
const (
	RedditSortHot    = "hot"
	RedditSortNew    = "new"
	RedditSortTop    = "top"
	RedditSortRising = "rising"
)

// Reddit time filters (for "top" sort)
const (
	TimeFilterHour  = "hour"
	TimeFilterDay   = "day"
	TimeFilterWeek  = "week"
	TimeFilterMonth = "month"
	TimeFilterYear  = "year"
	TimeFilterAll   = "all"
)

// Semantic Scholar modes
const (
	S2ModeSearch          = "search"
	S2ModeRecommendations = "recommendations"
)

// Config field keys
const (
	ConfigKeySubreddit    = "subreddit"
	ConfigKeySort         = "sort"
	ConfigKeyTimeFilter   = "time_filter"
	ConfigKeyLimit        = "limit"
	ConfigKeyMinScore     = "min_score"
	ConfigKeyMinComments  = "min_comments"
	ConfigKeyUserAgent    = "user_agent"
	ConfigKeyMode         = "mode"
	ConfigKeyQuery        = "query"
	ConfigKeyPaperID      = "paper_id"
	ConfigKeyMaxResults   = "max_results"
	ConfigKeyMinCitations = "min_citations"
	ConfigKeyYear         = "year"
)
