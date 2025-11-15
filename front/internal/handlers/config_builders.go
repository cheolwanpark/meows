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
	limit, err := parseIntWithBounds(r.FormValue("limit"), 25, 1, 100, "limit")
	if err != nil {
		return nil, err
	}
	config["limit"] = limit

	// Required: min_score (with default and bounds)
	minScore, err := parseIntWithBounds(r.FormValue("min_score"), 10, 0, 10000, "min_score")
	if err != nil {
		return nil, err
	}
	config["min_score"] = minScore

	// Required: min_comments (with default and bounds)
	minComments, err := parseIntWithBounds(r.FormValue("min_comments"), 5, 0, 1000, "min_comments")
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

	// Required: rate_limit_delay_ms (with default and bounds)
	rateLimitDelay, err := parseIntWithBounds(r.FormValue("rate_limit_delay_ms"), 2000, 100, 60000, "rate_limit_delay_ms")
	if err != nil {
		return nil, err
	}
	config["rate_limit_delay_ms"] = rateLimitDelay

	// Optional: OAuth credentials (all or none)
	clientID := r.FormValue("oauth_client_id")
	clientSecret := r.FormValue("oauth_client_secret")
	username := r.FormValue("oauth_username")
	password := r.FormValue("oauth_password")

	oauthFieldsProvided := 0
	if clientID != "" {
		oauthFieldsProvided++
	}
	if clientSecret != "" {
		oauthFieldsProvided++
	}
	if username != "" {
		oauthFieldsProvided++
	}
	if password != "" {
		oauthFieldsProvided++
	}

	if oauthFieldsProvided > 0 {
		if oauthFieldsProvided != 4 {
			return nil, errors.New("if providing OAuth credentials, all 4 fields (client_id, client_secret, username, password) are required")
		}
		config["oauth"] = map[string]string{
			"client_id":     clientID,
			"client_secret": clientSecret,
			"username":      username,
			"password":      password,
		}
	}

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
	maxResults, err := parseIntWithBounds(r.FormValue("max_results"), 25, 1, 100, "max_results")
	if err != nil {
		return nil, err
	}
	config["max_results"] = maxResults

	// Required: min_citations (with default and bounds)
	minCitations, err := parseIntWithBounds(r.FormValue("min_citations"), 10, 0, 100000, "min_citations")
	if err != nil {
		return nil, err
	}
	config["min_citations"] = minCitations

	// Required: rate_limit_delay_ms (with default and bounds)
	rateLimitDelay, err := parseIntWithBounds(r.FormValue("rate_limit_delay_ms"), 1000, 100, 60000, "rate_limit_delay_ms")
	if err != nil {
		return nil, err
	}
	config["rate_limit_delay_ms"] = rateLimitDelay

	// Optional: year filter (with format validation)
	year := r.FormValue("year")
	if year != "" {
		// Validate year format: YYYY or YYYY-YYYY
		if !isValidYearFormat(year) {
			return nil, errors.New("year must be in YYYY or YYYY-YYYY format")
		}
		config["year"] = year
	}

	// Optional: API key
	apiKey := r.FormValue("api_key")
	if apiKey != "" {
		config["api_key"] = apiKey
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

// parseIntOrDefault parses a string to int, returning defaultVal if empty
// Deprecated: Use parseIntWithBounds for better validation
func parseIntOrDefault(s string, defaultVal int) int {
	if s == "" {
		return defaultVal
	}
	val, err := strconv.Atoi(s)
	if err != nil {
		return defaultVal
	}
	return val
}

// isValidYearFormat validates year format as YYYY or YYYY-YYYY
func isValidYearFormat(year string) bool {
	// Match YYYY or YYYY-YYYY format (e.g., "2024" or "2020-2024")
	matched, _ := regexp.MatchString(`^\d{4}(-\d{4})?$`, year)
	return matched
}
