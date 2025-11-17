# Meows 🐾

A lightweight, self-hosted news aggregator that collects articles from Reddit and Semantic Scholar with scheduled crawling.

## Features

- Multi-source support (Reddit, Semantic Scholar)
- Global cron scheduling for automated collection
- YAML-based configuration with file-based credentials
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
# Clone the repository
git clone <repository-url>
cd meows

# Create data directory
mkdir -p data

# Create and configure .env
cp .env.example .env
chmod 600 .env

# Edit .env with your credentials
# 1. Add Reddit OAuth credentials from https://www.reddit.com/prefs/apps
# 2. Add Semantic Scholar API key from https://www.semanticscholar.org/product/api
vim .env

# Start services
docker compose up -d
```

**Access:**
- Frontend: http://localhost:3000
- API: http://localhost:8080
- API Docs: http://localhost:8080/swagger/index.html

## Configuration

Meows uses environment variables configured through a `.env` file for all settings.

**Setup:**
```bash
# Copy example and set permissions
cp .env.example .env
chmod 600 .env  # Protect credentials
```

**Configuration structure** (`.env`):
- **Collector settings**: Server port, database path, logging, Swagger UI
- **Global schedule**: Cron expression for all sources (e.g., `0 */6 * * *` = every 6 hours)
- **Rate limits**: Delays between API requests per source type (Reddit, Semantic Scholar)
- **Credentials**: Reddit OAuth credentials and Semantic Scholar API key (shared by all sources)
- **Frontend settings**: Server port, collector URL

See `.env.example` for detailed documentation of all environment variables.

**Important:**
- ⚠️ **Security**: Credentials are stored in plain text. Use `chmod 600 .env` to restrict access.
- 🔄 **Restart required**: Config changes require `docker compose restart` to take effect.
- 📝 **Format**: Use KEY=value format, no quotes needed unless value contains spaces.

**Adding sources:**
1. Configure global credentials in `.env`
2. Go to Sources page in the web UI
3. Select type (Reddit/Semantic Scholar) and configure filters
4. Add source (uses global credentials from config file)

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
