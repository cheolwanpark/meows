# Crawler

A minimal Rust CLI tool for crawling Reddit posts with keyword filtering.

## Features

- Fetch hot posts from Reddit subreddits
- Filter posts by keywords (case-insensitive)
- Support for ANY (OR) or ALL (AND) keyword matching
- Output as JSON to stdout or file
- Simple TOML configuration
- Extensible architecture for adding more sources

## Installation

```bash
cargo build --release
```

## Configuration

Create a `config.toml` file (see `config.toml.example` for reference):

```toml
[output]
destination = "stdout"  # or a file path like "/tmp/output.json"

[filter]
keywords = ["rust", "async"]
match_mode = "any"  # "any" or "all"

[sources.reddit]
subreddit = "rust"
limit = 25
```

### Configuration Options

#### `[output]`
- `destination`: Where to write output ("stdout" or file path)

#### `[filter]`
- `keywords`: List of keywords to search for (case-insensitive)
- `match_mode`:
  - `"any"` - Match posts containing at least one keyword (OR logic)
  - `"all"` - Match posts containing all keywords (AND logic)

#### `[sources.reddit]`
- `subreddit`: Target subreddit name (without /r/ prefix)
- `limit`: Maximum posts to fetch (1-100)

## Usage

### Basic usage with default config
```bash
cargo run
```

### Use custom config file
```bash
cargo run -- --config my-config.toml
```

### Override output destination
```bash
cargo run -- --output /tmp/results.json
```

### Example: Pipe to jq
```bash
cargo run | jq '.[].title'
```

## Output Format

The crawler outputs JSON array of posts:

```json
[
  {
    "id": "abc123",
    "title": "Post title",
    "body": "Post content (selftext)",
    "url": "https://...",
    "author": "username",
    "created_utc": 1234567890,
    "score": 100,
    "num_comments": 25,
    "subreddit": "rust"
  }
]
```

## Testing

Run tests:
```bash
cargo test
```

## Architecture

```
src/
├── main.rs       - CLI and orchestration
├── config.rs     - TOML configuration parsing and validation
├── reddit.rs     - Reddit API client
├── filter.rs     - Keyword filtering logic
└── output.rs     - JSON output formatting
```

### Adding New Sources

The architecture is designed to be extensible. To add a new source:

1. Define your source struct with fetch logic
2. Map your source's data to the `Content` struct
3. Add configuration schema to `config.rs`
4. Update `main.rs` to support the new source

## Notes

- The crawler respects Reddit's API by using a custom User-Agent
- No authentication required (uses public JSON endpoints)
- Rate limiting: Reddit may throttle requests if too frequent
- Stderr is used for progress messages, stdout for JSON output

## Future Enhancements

- Add BM25 ranking for better keyword matching
- Support multiple subreddits in parallel
- Add more content sources (Hacker News, etc.)
- OAuth authentication for higher rate limits
