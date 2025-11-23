package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
)

// buildRedditConfig builds and validates a Reddit source configuration from form data
func buildRedditConfig(r *http.Request) (map[string]interface{}, error) {
	config := make(map[string]interface{})

	// Required: subreddit
	subreddit := r.FormValue("subreddit")
	if subreddit == "" {
		return nil, errors.New("subreddit is required")
	}
	config["subreddit"] = subreddit

	// Required: sort (with validation)
	sort := r.FormValue("sort")
	if sort == "" {
		sort = "hot" // Default
	}
	validSorts := map[string]bool{"hot": true, "new": true, "top": true, "rising": true}
	if !validSorts[sort] {
		return nil, errors.New("sort must be one of: hot, new, top, rising")
	}
	config["sort"] = sort

	// Optional: time_filter (only for "top" sort)
	if sort == "top" {
		timeFilter := r.FormValue("time_filter")
		if timeFilter != "" {
			validFilters := map[string]bool{"hour": true, "day": true, "week": true, "month": true, "year": true, "all": true}
			if !validFilters[timeFilter] {
				return nil, errors.New("time_filter must be one of: hour, day, week, month, year, all")
			}
			config["time_filter"] = timeFilter
		}
	}

	// Required: limit (with default and bounds)
	limit, err := parseIntWithBounds(r.FormValue("limit"), RedditDefaultLimit, RedditMinLimit, RedditMaxLimit, "limit")
	if err != nil {
		return nil, err
	}
	config["limit"] = limit

	// Required: min_score (with default and bounds)
	minScore, err := parseIntWithBounds(r.FormValue("min_score"), RedditDefaultMinScore, RedditMinMinScore, RedditMaxMinScore, "min_score")
	if err != nil {
		return nil, err
	}
	config["min_score"] = minScore

	// Required: min_comments (with default and bounds)
	minComments, err := parseIntWithBounds(r.FormValue("min_comments"), RedditDefaultMinComments, RedditMinMinComments, RedditMaxMinComments, "min_comments")
	if err != nil {
		return nil, err
	}
	config["min_comments"] = minComments

	// Required: user_agent (with default)
	userAgent := r.FormValue("user_agent")
	if userAgent == "" {
		userAgent = "meows/1.0"
	}
	config["user_agent"] = userAgent

	return config, nil
}

// buildSemanticScholarConfig builds and validates a Semantic Scholar source configuration from form data
func buildSemanticScholarConfig(r *http.Request) (map[string]interface{}, error) {
	config := make(map[string]interface{})

	// Required: mode (search or recommendations)
	mode := r.FormValue("mode")
	if mode == "" {
		return nil, errors.New("mode is required (search or recommendations)")
	}
	if mode != "search" && mode != "recommendations" {
		return nil, errors.New("mode must be either 'search' or 'recommendations'")
	}
	config["mode"] = mode

	// Conditional required fields based on mode
	if mode == "search" {
		query := r.FormValue("query")
		if query == "" {
			return nil, errors.New("query is required when mode is 'search'")
		}
		config["query"] = query
	} else if mode == "recommendations" {
		paperID := r.FormValue("paper_id")
		if paperID == "" {
			return nil, errors.New("paper_id is required when mode is 'recommendations'")
		}
		config["paper_id"] = paperID
	}

	// Required: max_results (with default and bounds)
	maxResults, err := parseIntWithBounds(r.FormValue("max_results"), S2DefaultMaxResults, S2MinMaxResults, S2MaxMaxResults, "max_results")
	if err != nil {
		return nil, err
	}
	config["max_results"] = maxResults

	// Required: min_citations (with default and bounds)
	minCitations, err := parseIntWithBounds(r.FormValue("min_citations"), S2DefaultMinCitations, S2MinMinCitations, S2MaxMinCitations, "min_citations")
	if err != nil {
		return nil, err
	}
	config["min_citations"] = minCitations

	// Optional: year filter (with format validation)
	year := r.FormValue("year")
	if year != "" {
		// Validate year format: YYYY or YYYY-YYYY
		if !isValidYearFormat(year) {
			return nil, errors.New("year must be in YYYY or YYYY-YYYY format")
		}
		config["year"] = year
	}

	return config, nil
}

// buildHackerNewsConfig builds and validates a Hacker News source configuration from form data
func buildHackerNewsConfig(r *http.Request) (map[string]interface{}, error) {
	config := make(map[string]interface{})

	// Required: item_type (with validation)
	itemType := r.FormValue("item_type")
	if itemType == "" {
		itemType = "top" // Default
	}
	validTypes := map[string]bool{
		"top": true, "new": true, "best": true,
		"ask": true, "show": true, "job": true,
	}
	if !validTypes[itemType] {
		return nil, errors.New("item_type must be one of: top, new, best, ask, show, job")
	}
	config["item_type"] = itemType

	// Required: limit (with default and bounds)
	limit, err := parseIntWithBounds(r.FormValue("limit"), HNDefaultLimit, HNMinLimit, HNMaxLimit, "limit")
	if err != nil {
		return nil, err
	}
	config["limit"] = limit

	// Required: min_score (with default and bounds)
	minScore, err := parseIntWithBounds(r.FormValue("min_score"), HNDefaultMinScore, HNMinMinScore, HNMaxMinScore, "min_score")
	if err != nil {
		return nil, err
	}
	config["min_score"] = minScore

	// Required: min_comments (with default and bounds)
	minComments, err := parseIntWithBounds(r.FormValue("min_comments"), HNDefaultMinComments, HNMinMinComments, HNMaxMinComments, "min_comments")
	if err != nil {
		return nil, err
	}
	config["min_comments"] = minComments

	// Optional: include_comments (checkbox, default: false when unchecked)
	includeComments := r.FormValue("include_comments")
	// Checkbox sends "on" when checked, "" when unchecked
	config["include_comments"] = includeComments == "on" || includeComments == "true"

	// Conditional: comment-related fields only if include_comments is true
	if config["include_comments"].(bool) {
		maxCommentDepth, err := parseIntWithBounds(r.FormValue("max_comment_depth"), HNDefaultMaxCommentDepth, HNMinMaxCommentDepth, HNMaxMaxCommentDepth, "max_comment_depth")
		if err != nil {
			return nil, err
		}
		config["max_comment_depth"] = maxCommentDepth

		maxCommentsPerArticle, err := parseIntWithBounds(r.FormValue("max_comments_per_article"), HNDefaultMaxCommentsPerArticle, HNMinMaxCommentsPerArticle, HNMaxMaxCommentsPerArticle, "max_comments_per_article")
		if err != nil {
			return nil, err
		}
		config["max_comments_per_article"] = maxCommentsPerArticle
	}

	return config, nil
}

// parseIntWithBounds parses a string to int with validation
func parseIntWithBounds(s string, defaultVal, min, max int, fieldName string) (int, error) {
	if s == "" {
		return defaultVal, nil
	}
	val, err := strconv.Atoi(s)
	if err != nil {
		return 0, errors.New(fieldName + " must be a valid number")
	}
	if val < min || val > max {
		return 0, fmt.Errorf("%s must be between %d and %d", fieldName, min, max)
	}
	return val, nil
}

// isValidYearFormat validates year format as YYYY or YYYY-YYYY
func isValidYearFormat(year string) bool {
	// Match YYYY or YYYY-YYYY format (e.g., "2024" or "2020-2024")
	matched, _ := regexp.MatchString(`^\d{4}(-\d{4})?$`, year)
	return matched
}
