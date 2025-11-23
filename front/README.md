# Meows Frontend

Go + templ + htmx + Alpine.js frontend for the Meows news aggregator.

## Technology Stack

- **Go 1.21+**: Backend server
- **templ**: Type-safe HTML templating
- **htmx 1.9.12**: Dynamic HTML without JavaScript
- **Alpine.js 3.x**: Minimal JavaScript for UI state
- **Tailwind CSS v3**: Utility-first CSS
- **Chi router**: HTTP routing

## Quick Start

### Prerequisites

- Go 1.21 or higher
- Node.js & npm (for Tailwind CSS)

### Installation

```bash
# Install all dependencies (Go modules, npm packages, and tools)
make install
```

### Development

**Option 1: Using Makefile (Recommended)**

```bash
# Terminal 1: Start development server with hot reload
make dev

# Terminal 2: Watch and rebuild CSS on changes
make css-watch
```

**Option 2: Manual**

```bash
# Terminal 1: Generate templ components
templ generate --watch

# Terminal 2: Build Tailwind CSS
npm run dev:css

# Terminal 3: Run Go server
go run cmd/server/main.go
```

The server will start on **http://localhost:3000**

### First Time Setup

1. **Configure environment:**
   Set environment variables (see root `.env.example` for all options):
   - `FRONTEND_PORT`: HTTP server port (default: 3000)
   - `FRONTEND_COLLECTOR_URL`: Collector API URL (default: http://collector:8080)
   - `CSRF_KEY`: Secret for CSRF tokens (required for local development)

2. **Start the collector service:**
   The frontend requires the collector service to be running. See `../collector/` for instructions.

3. **Access the application:**
   - Home page: http://localhost:3000
   - Sources management: http://localhost:3000/config

### Building for Production

```bash
# Generate templ components
templ generate

# Build Tailwind CSS (minified)
npm run build:css

# Build Go binary
go build -o bin/server cmd/server/main.go

# Run
./bin/server
```

## Project Structure

```
front/
├── cmd/
│   └── server/          # Main entry point
├── internal/
│   ├── handlers/        # HTTP request handlers
│   ├── collector/       # Collector API client
│   ├── middleware/      # HTTP middleware (CSRF, etc.)
│   └── models/          # View models and helpers
├── templates/
│   ├── layouts/         # Base layouts (HTML shell, header, footer)
│   ├── pages/           # Full pages (home, config)
│   └── components/      # Reusable components
├── static/
│   ├── css/             # Tailwind CSS output
│   ├── js/              # htmx, Alpine.js
│   └── icons/           # Favicons, images
├── API_CONTRACTS.md     # API documentation
└── README.md           # This file
```

## API Integration

The frontend communicates with the **collector** service to fetch articles and manage sources. All API calls happen server-side; the browser never directly accesses the collector API.

See [API_CONTRACTS.md](./API_CONTRACTS.md) for detailed API documentation.

## Key Features

- **Server-Side Rendering**: Full HTML pages rendered by Go
- **Dynamic Updates**: htmx for AJAX interactions without full page reloads
- **Type Safety**: templ provides compile-time type checking for templates
- **Security**: CSRF protection, input validation, XSS prevention
- **Progressive Enhancement**: Works without JavaScript (htmx/Alpine.js degrade gracefully)

## Environment Variables

See root `.env.example` for all available configuration options.

| Variable | Default | Description |
|----------|---------|-------------|
| `FRONTEND_PORT` | 3000 | HTTP server port |
| `FRONTEND_COLLECTOR_URL` | http://collector:8080 | Collector API base URL |
| `CSRF_KEY` | (required) | Secret key for CSRF tokens |

## Development Workflow

1. Make changes to `.templ` files
2. templ generates `*_templ.go` files automatically (in watch mode)
3. Go server rebuilds and restarts (with air)
4. Tailwind CSS rebuilds on CSS/template changes
5. Refresh browser to see changes

## License

MIT
