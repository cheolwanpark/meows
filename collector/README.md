# Meows Collector

A Go-based web service for scheduled crawling and collecting articles from Reddit and Semantic Scholar, with persistent SQLite storage and REST API access.

## Features

- **Multi-source support**: Reddit (with comment fetching) and Semantic Scholar
- **Scheduled crawling**: Per-source cron scheduling with dynamic job management
- **REST API**: Full CRUD operations for sources and article queries
- **Persistent storage**: SQLite with WAL mode for concurrent reads
- **Low memory usage**: Optimized connection pooling and streaming inserts
- **Graceful shutdown**: Waits for in-flight jobs before terminating
- **Health & metrics**: Built-in monitoring endpoints

## Architecture

```
collector/
├── cmd/server/          # Main application entry point
├── internal/
│   ├── api/             # HTTP handlers and router (Chi)
│   ├── config/          # Environment configuration
│   ├── db/              # Database schema and models
│   ├── scheduler/       # Cron job management (robfig/cron)
│   └── source/          # Crawler implementations
│       ├── reddit.go            # Reddit API client
│       └── semantic_scholar.go  # Semantic Scholar API client
└── README.md
```

## Installation

### Prerequisites

- Go 1.21 or higher

### Build

```bash
cd collector
go mod tidy
go build -o bin/collector ./cmd/server
```

### Run

```bash
# With default configuration
./bin/collector

# With custom configuration
export DB_PATH="./data/meows.db"
export PORT=3000
export MAX_COMMENT_DEPTH=3
export LOG_LEVEL=debug
./bin/collector
```

## Configuration

Environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `DB_PATH` | `./meows.db` | Path to SQLite database file |
| `PORT` | `8080` | HTTP server port |
| `MAX_COMMENT_DEPTH` | `5` | Maximum Reddit comment depth to fetch |
| `LOG_LEVEL` | `info` | Log level: `debug`, `info`, `warn`, `error` |

## API Reference

### Sources

#### Create Source

**POST /sources**

Add a new crawling source with cron schedule.

**Reddit Example:**
```json
{
  "type": "reddit",
  "cron_expr": "0 */6 * * *",
  "config": {
    "subreddit": "golang",
    "sort": "hot",
    "limit": 100,
    "min_score": 10,
    "min_comments": 5,
    "user_agent": "meows-collector/1.0",
    "rate_limit_delay_ms": 1000
  }
}
```

**Semantic Scholar Search Example:**
```json
{
  "type": "semantic_scholar",
  "cron_expr": "0 0 * * *",
  "config": {
    "mode": "search",
    "query": "large language models",
    "year": "2024",
    "max_results": 50,
    "min_citations": 10,
    "api_key": "YOUR_API_KEY",
    "rate_limit_delay_ms": 1000
  }
}
```

**Semantic Scholar Recommendations Example:**
```json
{
  "type": "semantic_scholar",
  "cron_expr": "0 12 * * *",
  "config": {
    "mode": "recommendations",
    "paper_id": "649def34f8be52c8b66281af98ae884c09aef38b",
    "max_results": 20,
    "min_citations": 5,
    "rate_limit_delay_ms": 1000
  }
}
```

**Response:** `201 Created`
```json
{
  "id": "uuid",
  "type": "reddit",
  "config": {...},
  "cron_expr": "0 */6 * * *",
  "external_id": "golang",
  "status": "idle",
  "created_at": "2024-11-15T10:00:00Z"
}
```

#### List Sources

**GET /sources?type={type}**

Query parameters:
- `type` (optional): Filter by source type (`reddit` or `semantic_scholar`)

**Response:** `200 OK`
```json
[
  {
    "id": "uuid",
    "type": "reddit",
    "cron_expr": "0 */6 * * *",
    "external_id": "golang",
    "last_run_at": "2024-11-15T12:00:00Z",
    "last_success_at": "2024-11-15T12:00:00Z",
    "last_error": null,
    "status": "idle",
    "created_at": "2024-11-15T10:00:00Z"
  }
]
```

#### Get Source

**GET /sources/{id}**

**Response:** `200 OK` (same as individual source object above)

#### Update Source

**PUT /sources/{id}**

Update source configuration and/or schedule. Jobs are automatically rescheduled.

```json
{
  "config": {...},
  "cron_expr": "0 */12 * * *"
}
```

**Response:** `200 OK`

#### Delete Source

**DELETE /sources/{id}**

Removes source by UUID and cascades to delete associated articles and comments.

**Response:** `204 No Content`

#### Delete Source by Type and External ID

**DELETE /sources/{type}/{external_id}**

Alternative deletion method using the same identifiers used during creation (subreddit name, search query, or paper ID).

**Parameters:**
- `type`: Source type (`reddit` or `semantic_scholar`)
- `external_id`: Subreddit name, search query, or paper ID (URL-encoded if contains special characters)

**Examples:**
```bash
# Delete Reddit source for r/golang
curl -X DELETE http://localhost:8080/sources/reddit/golang

# Delete Semantic Scholar search (URL-encode spaces)
curl -X DELETE http://localhost:8080/sources/semantic_scholar/large%20language%20models

# Delete S2 recommendations by paper ID
curl -X DELETE http://localhost:8080/sources/semantic_scholar/649def34f8be52c8b66281af98ae884c09aef38b
```

**Response:**
- `204 No Content` - Source deleted successfully
- `400 Bad Request` - Invalid type or external_id contains slashes
- `404 Not Found` - Source not found
- `500 Internal Server Error` - Database error

**Note:** External IDs with special characters (spaces, etc.) must be URL-encoded. The endpoint automatically cascades to delete associated articles and comments.

### Schedule

#### Get Schedule

**GET /schedule**

Returns jobs scheduled to run in the next 24 hours.

**Response:** `200 OK`
```json
[
  {
    "source_id": "uuid",
    "source_type": "reddit",
    "next_run": "2024-11-15T18:00:00Z",
    "last_run_at": "2024-11-15T12:00:00Z"
  }
]
```

### Articles

#### List Articles

**GET /articles?source_id={id}&limit={n}&offset={n}&since={timestamp}**

Query parameters:
- `source_id` (optional): Filter by source ID
- `limit` (optional): Results per page (default: 50, max: 500)
- `offset` (optional): Pagination offset (default: 0)
- `since` (optional): Filter articles written after this timestamp (RFC3339 format)

**Response:** `200 OK`
```json
[
  {
    "id": "uuid",
    "source_id": "source-uuid",
    "external_id": "abc123",
    "title": "Article title",
    "author": "author_name",
    "content": "Article content...",
    "url": "https://reddit.com/r/golang/comments/abc123",
    "written_at": "2024-11-15T08:00:00Z",
    "metadata": {
      "score": 150,
      "num_comments": 25,
      "subreddit": "golang"
    },
    "created_at": "2024-11-15T12:00:00Z"
  }
]
```

### Monitoring

#### Health Check

**GET /health**

**Response:** `200 OK` (healthy) or `503 Service Unavailable` (unhealthy)
```json
{
  "status": "healthy",
  "database": "ok",
  "scheduler": "ok",
  "timestamp": "2024-11-15T12:00:00Z"
}
```

#### Metrics

**GET /metrics**

**Response:** `200 OK`
```json
{
  "total_sources": 10,
  "total_articles": 1523,
  "articles_today": 45,
  "sources_with_errors": 1,
  "last_crawl": "2024-11-15T12:00:00Z",
  "timestamp": "2024-11-15T12:05:00Z"
}
```

## Cron Expression Format

Standard cron format (5 fields):

```
┌───────────── minute (0 - 59)
│ ┌───────────── hour (0 - 23)
│ │ ┌───────────── day of month (1 - 31)
│ │ │ ┌───────────── month (1 - 12)
│ │ │ │ ┌───────────── day of week (0 - 6) (Sunday to Saturday)
│ │ │ │ │
│ │ │ │ │
* * * * *
```

**Examples:**
- `0 */6 * * *` - Every 6 hours
- `0 0 * * *` - Daily at midnight
- `0 12 * * MON` - Mondays at noon
- `*/15 * * * *` - Every 15 minutes

## Database Schema

### sources
- `id` - Primary key (UUID)
- `type` - Source type (reddit | semantic_scholar)
- `config` - JSON configuration
- `cron_expr` - Cron schedule
- `external_id` - Deduplication key (subreddit | query | paper_id)
- `last_run_at` - Last job execution time
- `last_success_at` - Last successful fetch
- `last_error` - Error message from last failed run
- `status` - Job status (idle | running)
- `created_at` - Creation timestamp

**Indexes:** `type`, `status`

### articles
- `id` - Primary key (UUID)
- `source_id` - Foreign key to sources
- `external_id` - Source-specific ID (Reddit post ID | S2 paper ID)
- `title` - Article title
- `author` - Primary author
- `content` - Article content/abstract
- `url` - Source URL
- `written_at` - Original publication date
- `metadata` - JSON (source-specific data)
- `created_at` - Crawl timestamp

**Unique constraint:** `(source_id, external_id)`
**Indexes:** `(source_id, written_at DESC)`, `created_at DESC`

### comments
- `id` - Primary key (UUID)
- `article_id` - Foreign key to articles
- `external_id` - Reddit comment ID
- `author` - Comment author
- `content` - Comment text
- `written_at` - Comment timestamp
- `parent_id` - Parent comment ID (NULL for top-level)
- `depth` - Comment depth (0 = top-level)

**Unique constraint:** `(article_id, external_id)`
**Index:** `article_id`

## Reddit Configuration Options

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `subreddit` | string | Yes | Subreddit name (without /r/) |
| `sort` | string | Yes | Sort mode: `hot`, `new`, `top`, `rising` |
| `time_filter` | string | No | For `top` sort: `hour`, `day`, `week`, `month`, `year`, `all` |
| `limit` | int | Yes | Max posts to fetch per run |
| `min_score` | int | Yes | Minimum post score |
| `min_comments` | int | Yes | Minimum comment count |
| `user_agent` | string | Yes | Reddit API user agent |
| `rate_limit_delay_ms` | int | Yes | Delay between requests (ms) |
| `oauth` | object | No | OAuth credentials (for authenticated API) |

## Semantic Scholar Configuration Options

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `mode` | string | Yes | `search` or `recommendations` |
| `query` | string | Conditional | Search query (required for search mode) |
| `paper_id` | string | Conditional | Paper ID (required for recommendations mode) |
| `year` | string | No | Year filter for search (e.g., "2024" or "2020-2024") |
| `max_results` | int | Yes | Maximum papers to fetch |
| `min_citations` | int | Yes | Minimum citation count |
| `api_key` | string | No | S2 API key (recommended for higher rate limits) |
| `rate_limit_delay_ms` | int | Yes | Delay between requests (ms) |

## Rate Limiting

- **Reddit**: 60 requests/minute (unauthenticated), 600/minute (with OAuth)
- **Semantic Scholar**: 1 request/second (no key), 100 requests/minute (with API key)

The crawler respects `Retry-After` headers and implements exponential backoff for rate limit errors.

## Memory Optimization

- SQLite connection pool limited to 10 concurrent connections
- 2 idle connections max
- Streaming article inserts (no buffering)
- Comment depth limiting
- WAL mode for concurrent reads without blocking

## Graceful Shutdown

1. HTTP server stops accepting new requests (30s timeout)
2. Scheduler waits for running jobs to complete (5min timeout)
3. Database connections closed
4. Signal: SIGINT (Ctrl+C) or SIGTERM

## Development

### Run from source
```bash
go run ./cmd/server
```

### Run tests
```bash
go test ./...
```

### Format code
```bash
go fmt ./...
```

## Troubleshooting

**Database locked errors:**
- Ensure only one collector instance per database file
- Check that WAL mode is enabled (automatic on initialization)
- Verify no other process has the database open

**Job not running:**
- Check cron expression with `crontab.guru`
- Verify source status is `idle`, not `running`
- Check `last_error` field in source object
- Review server logs

**Rate limit errors:**
- Increase `rate_limit_delay_ms` in source config
- For Reddit: Consider OAuth authentication
- For S2: Add API key to config
- Check `Retry-After` header in logs

## Migration from Rust Crawler

Key differences:
1. No CLI mode - all operations via REST API
2. No stdin config - sources registered via POST /sources
3. No keyword filtering - can be added as query parameter
4. Persistent storage instead of stdout JSON
5. Scheduled execution instead of one-off runs
6. Comments fetched for Reddit automatically

To migrate existing TOML configs:
1. Start collector service
2. Convert TOML to JSON and POST to /sources endpoint
3. Schedule will run automatically per cron expression

## License

Part of the Meows project.
