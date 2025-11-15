# Known Issues & Limitations

This document tracks known issues and limitations in the current implementation.

## Recently Resolved Issues

### 1. Source Type Detection ✅ RESOLVED (2025-11-15)
**File:** `internal/handlers/handlers.go:57-80`
**Was:** Article rendering hardcoded source type to "reddit"
**Fixed:** Implemented source type lookup map to avoid N+1 queries

**Implementation:**
```go
// Fetch sources once and build lookup map
collectorSources, err := h.collector.GetSources(ctx)
sourceTypeMap := make(map[string]string)
for _, s := range collectorSources {
    sourceTypeMap[s.ID] = s.Type
}

// O(1) lookup for each article
sourceType := sourceTypeMap[a.SourceID]
if sourceType == "" {
    sourceType = "reddit" // Fallback for legacy/orphaned articles
}
```

### 2. Semantic Scholar Support ✅ RESOLVED (2025-11-15)
**Files:** `templates/components/add-source-form.templ`, `internal/handlers/handlers.go`, `internal/handlers/config_builders.go`
**Was:** Only Reddit sources supported
**Fixed:** Full multi-source support with progressive disclosure form

**Features Implemented:**
- Source type selector (Reddit vs Semantic Scholar)
- Conditional form fields using Alpine.js
- Progressive disclosure (required fields + collapsible advanced options)
- Separate credentials section with password masking
- Config builders with validation for both source types
- Proper error handling and field-specific validation

## Critical Issues

### 2. Pause/Resume Button Non-Functional
**File:** `templates/components/source-card.templ:47-63`
**Issue:** Button has Alpine.js state but no htmx endpoint
**Impact:** Misleading UX - users can click but nothing happens
**Fix Required:** Either implement `/config/sources/:id/toggle` endpoint or remove button

## Plan Deviations

### 1. go-humanize Dependency Unused
**Issue:** Dependency included but never imported
**Impact:** Unnecessary binary bloat
**Fix:** Either use `humanize.Time()` instead of custom `RelativeTime()` or remove dependency

## Security Limitations

### 1. No Rate Limiting
**Missing:** Rate limits on all endpoints
**Risk:** DoS vulnerability - users can spam create/delete requests
**Fix Required:** Add rate limiting middleware (e.g., github.com/ulule/limiter)

### 2. No Input Length Validation
**Missing:** Max length checks on form inputs
**Risk:** Memory exhaustion via large subreddit names or cron expressions
**Fix Required:** Add validation in `CreateSource` handler

### 3. CSRF Cookie Secure Flag
**Note:** Now set to `true` but may cause issues in local dev without HTTPS
**Workaround:** Use `http://localhost` (browsers treat localhost as secure context)

## Feature Gaps

### 1. Dark Mode Not Persisted
**File:** `templates/layouts/header.templ:22`
**Issue:** Alpine.js dark mode state resets on page reload
**Fix Required:** Save preference to localStorage

### 2. No htmx Loading Indicators
**File:** `templates/components/add-source-form.templ:81`
**Issue:** Loading spinner defined but not wired to htmx
**Fix Required:** Add `hx-indicator="#loading-spinner"` to form

### 3. Missing htmx Error Toasts
**Planned:** Alpine.js toast notifications on `htmx:responseError`
**Actual:** No error feedback on AJAX failures
**Fix Required:** Add global htmx error listener + toast component

## Code Quality Issues

### 1. Magic Numbers
**Locations:**
- `handlers.go:44` - `limit: 50` hardcoded
- `handlers.go:37,74,107,201` - `10*time.Second` repeated
- `handlers.go:147-155` - Config defaults in map

**Fix:** Extract to constants or config struct

### 2. Unused CSRF Fields
**File:** `internal/middleware/csrf.go:21-22`
**Issue:** `sync.Map` and `key` fields declared but never used
**Fix:** Remove if not needed, or implement token rotation

### 3. Inconsistent Error Messages
**File:** `internal/handlers/handlers.go:180`
**Issue:** Exposes internal errors to users (`err.Error()`)
**Fix:** Log technical error, show generic message to user

## Test Coverage

**Current:** 0% - No tests implemented
**Required:** Unit tests for:
- Collector client (with mocked HTTP)
- View model conversions
- Helper functions
- CSRF middleware
- Handler logic

**Integration tests for:**
- Full page rendering
- htmx interactions
- Form validation

## Performance Considerations

### 1. Source Type Lookup Inefficiency
When implemented, avoid N+1 query pattern:
```go
// BAD: N+1 queries
for _, article := range articles {
    sourceType := getSourceType(article.SourceID) // Query per article
}

// GOOD: Single query with join or map
sourceTypes := getSourceTypesMap(articleIDs)
for _, article := range articles {
    sourceType := sourceTypes[article.SourceID]
}
```

### 2. No Caching
Articles and sources fetched on every page load. Consider:
- Redis cache with TTL
- In-memory cache with expiration
- HTTP cache headers

## Documentation Gaps

### 1. No API Documentation for New Endpoints
If pause/resume is implemented, document in `API_CONTRACTS.md`

### 2. No Deployment Guide
Missing instructions for:
- Production build
- Environment variable management
- Reverse proxy setup (nginx/caddy)
- TLS certificate configuration

## Browser Compatibility

### 1. htmx 1.9.12 Features
Using `hx-swap="delete"` which is new in 1.9.12
**Fallback:** Change to `hx-swap="outerHTML"` with empty response for older htmx versions

### 2. Alpine.js 3.x
Requires modern browsers with ES6 support
**Note:** No IE11 support

## Future Improvements

1. **Pagination:** Articles list loads all 50 at once (should paginate)
2. **Search/Filter:** No way to filter articles by source or keyword
3. **Article Details:** No way to view article content or comments
4. **Source Stats:** Show article count, last successful crawl per source
5. **Bulk Operations:** Delete multiple sources at once
6. **Export:** Export articles to JSON/CSV
7. **Notifications:** Email/webhook when new articles arrive
8. **User Authentication:** Currently no auth (single-user assumed)

---

**Last Updated:** 2025-11-15
**Recent Changes:**
- ✅ Resolved Critical Issue #1: Source Type Detection
- ✅ Resolved Plan Deviation #1: Semantic Scholar Support
- Added multi-source form with progressive disclosure UI
- Implemented config builders with validation
- Updated article display to show citations for S2 papers

**Review Needed:** Consider adding unit tests for config builders and integration tests for multi-source workflows
