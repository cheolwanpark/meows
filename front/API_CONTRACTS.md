# API Contracts & Architecture

## Collector API Endpoints

Base URL: `http://localhost:8080` (configurable via `COLLECTOR_URL` env var)

### Articles

#### GET /articles
**Query Parameters:**
- `source_id` (optional): Filter by source UUID
- `limit` (optional): Max results (default: 50, max: 500)
- `offset` (optional): Pagination offset (default: 0)
- `since` (optional): RFC3339 timestamp filter

**Response:** `200 OK`
```json
[
  {
    "id": "uuid",
    "source_id": "uuid",
    "external_id": "string",
    "title": "string",
    "author": "string",
    "content": "string",
    "url": "string",
    "written_at": "2024-01-15T12:34:56Z",
    "metadata": {},
    "created_at": "2024-01-15T12:34:56Z"
  }
]
```

**Notes:**
- `url` may be empty string
- `metadata` is JSON (Reddit: `{score, num_comments}`, S2: `{citations, year}`)
- Ordered by `written_at DESC`

### Sources

#### GET /sources
**Query Parameters:**
- `type` (optional): Filter by "reddit" or "semantic_scholar"

**Response:** `200 OK`
```json
[
  {
    "id": "uuid",
    "type": "reddit",
    "config": {},
    "cron_expr": "0 */6 * * *",
    "external_id": "programming",
    "last_run_at": "2024-01-15T12:00:00Z",
    "last_success_at": "2024-01-15T12:00:00Z",
    "last_error": "",
    "status": "idle",
    "created_at": "2024-01-15T10:00:00Z"
  }
]
```

**Notes:**
- `config` exposes credentials (WARNING: sensitive data)
- `last_run_at`, `last_success_at`, `last_error` may be null/empty
- `status` is "idle" or "running"
- Ordered by `created_at DESC`

#### POST /sources
**Request Body:**
```json
{
  "type": "reddit",
  "config": {
    "subreddit": "programming",
    "sort": "hot",
    "limit": 25,
    "min_score": 10,
    "min_comments": 5,
    "user_agent": "meows/1.0",
    "rate_limit_delay_ms": 2000
  },
  "cron_expr": "0 */6 * * *"
}
```

**Response:** `201 Created` (same as Source object above)

**Error Responses:**
- `400 Bad Request`: Invalid type, invalid cron, invalid config
- `409 Conflict`: Duplicate (type, external_id) pair exists

#### DELETE /sources/:id
**Path Parameters:**
- `id`: Source UUID

**Response:** `204 No Content`

**Error Responses:**
- `404 Not Found`: Source doesn't exist

#### GET /schedule
**Response:** `200 OK`
```json
[
  {
    "source_id": "uuid",
    "source_type": "reddit",
    "next_run": "2024-01-15T18:00:00Z",
    "last_run_at": "2024-01-15T12:00:00Z"
  }
]
```

**Notes:**
- Returns upcoming jobs in next 24 hours

### Monitoring

#### GET /health
**Response:** `200 OK` or `503 Service Unavailable`
```json
{
  "status": "healthy",
  "database": "ok",
  "scheduler": "ok",
  "timestamp": "2024-01-15T12:34:56Z"
}
```

#### GET /metrics
**Response:** `200 OK`
```json
{
  "total_sources": 5,
  "total_articles": 1234,
  "articles_today": 45,
  "sources_with_errors": 1,
  "last_crawl": "2024-01-15T12:00:00Z",
  "timestamp": "2024-01-15T12:34:56Z"
}
```

---

## Frontend Routes & Handlers

### GET /
**Purpose:** Home page (article list)
**Data Fetching:** `GET /articles?limit=50`
**Template:** `pages/home.templ`
**Props:**
- `articles []Article`
- `error string` (if collector unavailable)

### GET /sources
**Purpose:** Source management page
**Data Fetching:** `GET /sources` (from collector)
**Template:** `pages/config.templ`
**Props:**
- `sources []Source`
- `csrfToken string`
- `error string` (if collector unavailable)

### POST /api/sources (htmx)
**Purpose:** Create new source
**Request:** Form data (name, url, category, cron)
**Data Mutation:** `POST /sources` on collector (proxied through frontend)
**Response:**
- Success: `200 OK` with SourceCard fragment
- Validation Error: `422 Unprocessable Entity` with form + error messages
**htmx Attributes:** `hx-post="/api/sources" hx-target="#source-list" hx-swap="afterbegin"`

### DELETE /api/sources/{id} (htmx)
**Purpose:** Delete source
**Data Mutation:** `DELETE /sources/{id}` on collector (proxied through frontend)
**Response:** `200 OK` empty body (htmx removes element)
**htmx Attributes:** `hx-delete="/api/sources/{id}" hx-target="closest .source-card" hx-swap="delete"`

---

## Templ Component Signatures

### Layouts

```templ
// Base HTML shell with head, htmx, Alpine.js
templ Base(title string, content templ.Component)

// Sticky header with navigation
templ Header(currentPath string)

// Simple footer
templ Footer()
```

### Components

```templ
// Individual news item card
templ NewsItem(article Article, index int)

// Source management card with actions
templ SourceCard(source Source, csrfToken string)

// List of source cards
templ SourceList(sources []Source, csrfToken string)

// Add source form
templ AddSourceForm(csrfToken string, errors map[string]string, values map[string]string)

// Error message display
templ ErrorMessage(message string)

// Empty state message
templ EmptyState(title string, description string)
```

### Pages

```templ
// Home page: article feed
templ HomePage(articles []Article)

// Config page: source management
templ ConfigPage(sources []Source, csrfToken string, errors map[string]string)

// Error page: when collector is down
templ ErrorPage(title string, message string)
```

---

## View Models

```go
// Article represents a news article for display
type Article struct {
    ID         string
    SourceID   string
    Title      string
    Author     string
    URL        string
    Domain     string    // Extracted from URL
    WrittenAt  time.Time
    TimeAgo    string    // "2 hours ago"
    Score      int       // From metadata
    Comments   int       // From metadata
    Source     string    // "reddit" or "semantic_scholar"
}

// Source represents a crawling source for display
type Source struct {
    ID          string
    Type        string // "reddit" or "semantic_scholar"
    Name        string // Human-friendly name (from config)
    URL         string // Subreddit URL or S2 query
    Category    string // "tech", "news", "science", "business", "other"
    CategoryEmoji string // Computed from category
    CronExpr    string
    Status      string // "idle" or "running"
    LastRunAt   *time.Time
    LastError   string
    IsActive    bool // Derived from status/errors
}

// FormErrors holds validation errors
type FormErrors struct {
    Name     string
    URL      string
    Category string
    Cron     string
    General  string // Non-field-specific errors
}
```

---

## HTMX Patterns

### Pattern 1: Form Submission with Validation
```html
<form hx-post="/api/sources" hx-target="#source-list" hx-swap="afterbegin">
  <!-- Form fields -->
</form>
```

**Success Response (200 OK):**
```html
<div class="source-card" id="source-{id}">
  <!-- SourceCard component HTML -->
</div>
```

**Validation Error Response (422 Unprocessable Entity):**
```html
<form hx-post="/api/sources" hx-target="#source-list" hx-swap="afterbegin">
  <div class="error">Subreddit name is required</div>
  <!-- Form fields with error styling -->
</form>
```

### Pattern 2: Delete with Confirmation
```html
<button
  hx-delete="/api/sources/{id}"
  hx-target="closest .source-card"
  hx-swap="delete"
  hx-confirm="Delete this source? All articles will be removed."
  class="btn-delete">
  Delete
</button>
```

**Handler Response (200 OK):**
- Empty body (htmx removes element automatically)

### Pattern 3: Loading States
```html
<button
  hx-post="/api/sources"
  hx-indicator="#spinner"
  class="btn-submit">
  Submit
  <span id="spinner" class="htmx-indicator">⏳</span>
</button>
```

**CSS:**
```css
.htmx-indicator { display: none; }
.htmx-request .htmx-indicator { display: inline; }
.htmx-request .btn { opacity: 0.6; pointer-events: none; }
```

### Pattern 4: Error Handling
**Network Error / 5xx Response:**
- htmx triggers `htmx:responseError` event
- Show toast notification with "Something went wrong"
- Use Alpine.js for toast state management

**4xx Client Error:**
- Return HTML fragment with error message
- htmx swaps it into target
- Use appropriate status code (422 for validation, 404 for not found)

---

## Helper Functions

### Time Formatting
```go
func RelativeTime(t time.Time) string {
    duration := time.Since(t)

    if duration < time.Minute {
        return "just now"
    } else if duration < time.Hour {
        minutes := int(duration.Minutes())
        return fmt.Sprintf("%d minute%s ago", minutes, pluralize(minutes))
    } else if duration < 24*time.Hour {
        hours := int(duration.Hours())
        return fmt.Sprintf("%d hour%s ago", hours, pluralize(hours))
    } else {
        days := int(duration.Hours() / 24)
        return fmt.Sprintf("%d day%s ago", days, pluralize(days))
    }
}
```

### Domain Extraction
```go
func ExtractDomain(rawURL string) string {
    if rawURL == "" {
        return ""
    }

    parsed, err := url.Parse(rawURL)
    if err != nil {
        return ""
    }

    // Remove www. prefix
    domain := strings.TrimPrefix(parsed.Hostname(), "www.")
    return domain
}
```

### Category Emoji
```go
func CategoryEmoji(category string) string {
    emojiMap := map[string]string{
        "tech":     "💻",
        "news":     "📰",
        "science":  "🔬",
        "business": "💼",
        "other":    "📌",
    }

    if emoji, ok := emojiMap[category]; ok {
        return emoji
    }
    return "📌"
}
```

### Parse Metadata
```go
func ParseRedditMetadata(metadata json.RawMessage) (score int, comments int) {
    var data struct {
        Score       int `json:"score"`
        NumComments int `json:"num_comments"`
    }

    json.Unmarshal(metadata, &data)
    return data.Score, data.NumComments
}
```

---

## Security Considerations

### CSRF Protection
1. **Token Generation:** Generate random token per session, store in cookie
2. **Token Injection:** Add hidden input to all forms + meta tag in `<head>`
3. **Token Validation:** Middleware checks token on POST/DELETE/PATCH
4. **htmx Integration:** htmx reads meta tag, includes in requests automatically

```html
<meta name="csrf-token" content="{token}">
<input type="hidden" name="csrf_token" value="{token}">
```

### Input Validation
1. **URL Validation:** Check scheme is http/https only
2. **Cron Validation:** Use `robfig/cron` parser to validate expressions
3. **Field Length:** Limit name (100 chars), URL (500 chars)
4. **Required Fields:** Name, URL, cron expression

### XSS Prevention
1. **templ Auto-Escaping:** All variables are HTML-escaped by default
2. **Safe URLs:** Use `templ.SafeURL()` for href attributes
3. **Metadata Sanitization:** JSON is stringified, not rendered as HTML

### Error Exposure
1. **Hide Internal Errors:** Never show database errors, stack traces to users
2. **Log Detailed Errors:** Use structured logging (slog) for debugging
3. **User-Friendly Messages:** "Something went wrong" instead of error details

---

## Configuration

### Environment Variables
```bash
# Frontend server
PORT=3000                          # HTTP server port
COLLECTOR_URL=http://localhost:8080 # Collector API base URL
CSRF_KEY=your-secret-key-here      # CSRF token signing key

# Development
ENV=development                     # "development" or "production"
LOG_LEVEL=info                      # "debug", "info", "warn", "error"
```

### Default Values
- Port: 3000
- Collector URL: http://localhost:8080
- Request timeout: 10 seconds
- CSRF token length: 32 bytes
- Articles per page: 50
