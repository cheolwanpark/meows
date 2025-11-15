# Quick Start Guide

## 🚀 Getting the Frontend Running

### Step 1: Install Dependencies

```bash
cd /Users/cheolwanpark/Documents/Projects/meows/front
make install
```

This will:
- Install Go dependencies
- Install npm packages (Tailwind CSS)
- Install `templ` CLI tool
- Install `air` for hot reload

### Step 2: Start the Collector Service

The frontend needs the collector API to be running. In a separate terminal:

```bash
cd /Users/cheolwanpark/Documents/Projects/meows/collector
go run cmd/server/main.go
```

The collector should start on `http://localhost:8080`.

### Step 3: Build the Frontend

```bash
cd /Users/cheolwanpark/Documents/Projects/meows/front
make build
```

This will:
- Generate templ Go files from `.templ` templates
- Build Tailwind CSS
- Compile the Go server

### Step 4: Run the Server

```bash
./bin/server
```

Or for development with hot reload:

```bash
# Terminal 1
make dev

# Terminal 2
make css-watch
```

### Step 5: Open in Browser

Visit: **http://localhost:3000**

- **Home page** (`/`): View aggregated articles
- **Sources page** (`/config`): Add and manage Reddit sources

---

## Adding Your First Source

1. Go to http://localhost:3000/config
2. Fill in the form:
   - **Subreddit Name**: e.g., `programming`
   - **Schedule**: e.g., `0 */6 * * *` (every 6 hours)
3. Click "Add Source 🐾"
4. Wait for the first crawl to complete (check collector logs)
5. Go to home page to see articles

---

## Troubleshooting

### Port Already in Use

If port 3000 is busy, change it in `.env`:
```bash
PORT=3001
```

### Collector Not Reachable

Make sure the collector is running:
```bash
curl http://localhost:8080/health
```

Should return:
```json
{"status":"healthy","database":"ok","scheduler":"ok","timestamp":"..."}
```

### Build Errors

Try cleaning and rebuilding:
```bash
make clean
make build
```

### CSS Changes Not Applying

Make sure Tailwind is watching for changes:
```bash
make css-watch
```

---

## Development Commands

| Command | Description |
|---------|-------------|
| `make help` | Show all available commands |
| `make install` | Install all dependencies |
| `make build` | Build the application |
| `make run` | Build and run the server |
| `make dev` | Run with hot reload (air) |
| `make css-watch` | Watch and rebuild CSS |
| `make clean` | Remove build artifacts |
| `make test` | Run tests |

---

## Project Structure

```
front/
├── cmd/server/main.go          # Entry point
├── internal/
│   ├── collector/              # Collector API client
│   ├── handlers/               # HTTP handlers
│   ├── middleware/             # CSRF middleware
│   └── models/                 # View models & helpers
├── templates/
│   ├── layouts/                # Base layout, header, footer
│   ├── pages/                  # Home & config pages
│   └── components/             # Reusable components
├── static/
│   ├── css/                    # Tailwind CSS (output.css)
│   ├── js/                     # htmx.min.js, alpine.min.js
│   └── icons/                  # Favicons
├── Makefile                    # Development tasks
├── .air.toml                   # Hot reload config
└── .env                        # Environment variables
```

---

## Next Steps

- Add more Reddit sources in the `/config` page
- Explore different subreddits (programming, news, science, etc.)
- Check the collector logs to see when sources are being crawled
- Articles will appear on the home page after the first successful crawl

Enjoy using Meows! 🐾
