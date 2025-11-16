# Meows 🐾

A lightweight, self-hosted news aggregator that collects articles from Reddit and Semantic Scholar with scheduled crawling and full-text search.

## Features

- **Multi-source support**: Reddit subreddits and Semantic Scholar papers
- **Scheduled crawling**: Configure global cron schedule for automated collection
- **Encrypted credentials**: AES-256-GCM encryption for API keys stored in database
- **Comment support**: Fetches and displays nested Reddit comments
- **Web UI**: Clean, responsive interface with dark mode
- **REST API**: Full API access with Swagger documentation
- **Docker deployment**: Single-command setup with Docker Compose

## Architecture

```
┌─────────────┐         ┌──────────────┐         ┌─────────────┐
│  Frontend   │────────>│  Collector   │────────>│   SQLite    │
│   (Go)      │         │   API (Go)   │         │     DB      │
│  Port 3000  │         │  Port 8080   │         └─────────────┘
└─────────────┘         └──────────────┘
                               │
                               ▼
                        ┌──────────────┐
                        │  Scheduler   │
                        │ (cron jobs)  │
                        └──────────────┘
```

## Quick Start

### Prerequisites

- Docker & Docker Compose
- 2GB RAM minimum
- Generate encryption keys (see below)

### 1. Clone and Configure

```bash
git clone <repository-url>
cd meows

# Copy environment template
cp .env.example .env

# Generate encryption key (exactly 32 bytes)
openssl rand -base64 32 | cut -c1-32

# Edit .env and set MEOWS_ENCRYPTION_KEY and CSRF_KEY
nano .env
```

### 2. Start Services

```bash
docker compose up -d
```

### 3. Access

- **Frontend**: http://localhost:3000
- **API**: http://localhost:8080
- **API Docs**: http://localhost:8080/swagger/index.html

## Configuration

### Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `MEOWS_ENCRYPTION_KEY` | ✅ | 32-byte encryption key for credentials |
| `CSRF_KEY` | ✅ | Random string for CSRF protection |
| `DB_PATH` | ❌ | Database file path (default: `/data/meows.db`) |
| `COLLECTOR_PORT` | ❌ | Collector API port (default: `8080`) |
| `FRONTEND_PORT` | ❌ | Frontend UI port (default: `3000`) |
| `LOG_LEVEL` | ❌ | Logging level (default: `info`) |

### Global Settings

Configure via Settings page (⚙️ icon) in the web UI:

- **Cron Schedule**: When to crawl sources (e.g., `0 */6 * * *` = every 6 hours)
- **Rate Limits**: Delay between API requests per source type
- **Credentials**: Reddit OAuth and Semantic Scholar API keys (optional but recommended)

### Adding Sources

1. Navigate to **Sources** page
2. Select source type (Reddit or Semantic Scholar)
3. Configure filters:
   - **Reddit**: Subreddit, sort method, post limits, min score/comments
   - **Semantic Scholar**: Search query or paper ID for recommendations
4. Click "Add Source"

## API Endpoints

### Frontend Public API (Port 3000)

All public API requests go through the frontend at `/api/*`. The collector service is internal only.

| Method | Endpoint | Description |
|--------|----------|-------------|
| POST | `/api/sources` | Create new source |
| DELETE | `/api/sources/{id}` | Delete source |
| PATCH | `/api/settings` | Update global settings |

### Collector Internal API (Port 8080, Docker network only)

The collector service is not exposed externally. It's accessible only via Docker network.

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/sources` | List all sources |
| POST | `/sources` | Create new source |
| DELETE | `/sources/{id}` | Delete source |
| GET | `/articles` | List articles (paginated) |
| GET | `/articles/{id}` | Get article with comments |
| GET | `/config` | Get global config (no credentials) |
| PATCH | `/config` | Update global config |
| GET | `/schedule` | Get next scheduled run |
| GET | `/health` | Health check |

**Note:** Collector Swagger documentation available only within Docker network

## Development

### Local Development (without Docker)

```bash
# Terminal 1: Start Collector
cd collector
export MEOWS_ENCRYPTION_KEY="your-32-byte-key-here-exactly!"
go run cmd/server/main.go

# Terminal 2: Start Frontend
cd front
export CSRF_KEY="your-random-csrf-key"
export COLLECTOR_URL="http://localhost:8080"
go run cmd/server/main.go
```

### Build from Source

```bash
# Build collector
cd collector
go build -o bin/collector cmd/server/main.go

# Build frontend (requires templ CLI)
cd front
templ generate
go build -o bin/frontend cmd/server/main.go
```

## Security Notes

- **Encryption**: All credentials encrypted at rest with AES-256-GCM
- **No auth**: Currently no authentication - deploy behind reverse proxy if exposing publicly
- **CSRF protection**: Enabled by default for all state-changing operations
- **Rate limiting**: Configurable per source type to respect API limits

## Troubleshooting

**Container exits immediately:**
```bash
# Check logs
docker compose logs collector

# Verify encryption key is exactly 32 bytes
echo -n "$MEOWS_ENCRYPTION_KEY" | wc -c  # Should output: 32
```

**Database permission errors:**
```bash
# Rebuild with clean volumes
docker compose down -v
docker compose up -d
```

**Sources not crawling:**
- Check cron schedule in Settings (must be valid cron expression)
- Verify global config saved successfully
- Check collector logs: `docker compose logs -f collector`

## License

MIT License - see LICENSE file for details

## Contributing

Contributions welcome! Please open an issue or PR.
