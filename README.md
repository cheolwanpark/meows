# Meows 🐾

A lightweight, self-hosted news aggregator that collects articles from Reddit and Semantic Scholar with scheduled crawling.

## Features

- Multi-source support (Reddit, Semantic Scholar)
- Global cron scheduling for automated collection
- AES-256-GCM encrypted credentials
- Nested Reddit comments
- Responsive web UI with dark mode
- REST API with Swagger documentation
- Docker deployment

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

```bash
# Clone and configure
git clone <repository-url>
cd meows
cp .env.example .env

# Generate keys and edit .env
openssl rand -base64 32 | cut -c1-32  # Copy to MEOWS_ENCRYPTION_KEY
openssl rand -hex 32                   # Copy to CSRF_KEY

# Start services
docker compose up -d
```

**Access:**
- Frontend: http://localhost:3000
- API: http://localhost:8080
- API Docs: http://localhost:8080/docs/index.html

## Configuration

**Required environment variables:**
- `MEOWS_ENCRYPTION_KEY` - 32-byte key for credential encryption
- `CSRF_KEY` - Random key for CSRF protection

**Optional:** `DB_PATH`, `FRONTEND_PORT`, `LOG_LEVEL`, `MAX_COMMENT_DEPTH`, `ENABLE_SWAGGER`

**Web UI settings** (⚙️ icon):
- Cron schedule (default: `0 */6 * * *`)
- Rate limits per source type
- API credentials (Reddit OAuth, Semantic Scholar)

**Adding sources:**
1. Go to Sources page
2. Select type and configure filters
3. Add source

## API Endpoints

**Frontend (Port 3000)** - Public API via `/api/*`:
- `POST /api/sources` - Create source
- `DELETE /api/sources/{id}` - Delete source
- `PATCH /api/settings` - Update settings

**Collector (Port 8080)** - Internal only (Docker network):
- `GET/POST/DELETE /sources` - Manage sources
- `GET /articles`, `GET /articles/{id}` - List/view articles
- `GET/PATCH /config` - Global configuration
- `GET /schedule`, `GET /health`, `GET /metrics` - Monitoring
- `GET /docs/*` - Swagger documentation

## Development

**Local development:**
```bash
# Collector
cd collector && export MEOWS_ENCRYPTION_KEY="your-32-byte-key" && go run cmd/server/main.go

# Frontend (separate terminal)
cd front && export CSRF_KEY="key" COLLECTOR_URL="http://localhost:8080" && go run cmd/server/main.go
```

**Build from source:**
```bash
cd collector && go build -o bin/collector cmd/server/main.go
cd front && templ generate && go build -o bin/frontend cmd/server/main.go
```

## Security

- AES-256-GCM encrypted credentials at rest
- No authentication (use reverse proxy for public deployment)
- CSRF protection enabled
- Configurable rate limiting

## Troubleshooting

```bash
# Container exits
docker compose logs collector
echo -n "$MEOWS_ENCRYPTION_KEY" | wc -c  # Must be 32

# Permission errors
docker compose down -v && docker compose up -d

# Sources not crawling
docker compose logs -f collector  # Check cron schedule in Settings
```

## License

MIT License - see LICENSE file for details

## Contributing

Contributions welcome! Please open an issue or PR.
