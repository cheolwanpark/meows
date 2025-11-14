# Crawler

A minimal Rust CLI tool for crawling content from multiple sources with keyword filtering. Currently supports Reddit with extensible architecture for additional sources.

## Features

- **Multiple source support**: Configure multiple Reddit sources (different subreddits, sorting methods) in parallel
- **Flexible sorting**: hot, new, top (with time filters), or rising posts
- **Pagination**: Automatically handles pagination to fetch any number of posts
- **Keyword filtering**: Case-insensitive substring matching with ANY/ALL modes
- **Config-based filters**: Per-source min_score and min_comments filtering
- **Rate limiting**: Configurable delays between requests
- **Concurrent fetching**: Fetch from multiple sources simultaneously
- **Stdin configuration**: Pipe-based config for better composability
- **JSON output**: Clean JSON output to stdout or file

## Installation

```bash
cargo build --release
```

## Configuration

Create a `config.toml` file (see `config.toml.example` for full reference):

```toml
[crawler]
user_agent = "crawler/0.1.0 (by /u/your_username)"  # REQUIRED by Reddit API
output_destination = "stdout"
max_concurrency = 5

[[sources]]
type = "reddit"
enabled = true
subreddit = "rust"
limit = 100
sort_by = "hot"
min_score = 0
min_comments = 0
user_agent = "crawler/0.1.0"
rate_limit_delay_ms = 1000

[[sources]]
type = "reddit"
enabled = true
subreddit = "programming"
limit = 50
sort_by = "new"
min_score = 10
min_comments = 2
user_agent = "crawler/0.1.0"
rate_limit_delay_ms = 1000
```

### Configuration Options

#### `[crawler]` - General Settings
- `user_agent`: User-Agent header (REQUIRED by Reddit API)
- `output_format`: Output format, currently "json" only
- `output_destination`: "stdout" or file path (can override with `--output`)
- `log_level`: error, warn, info, debug, trace (can override with `--log-level`)
- `max_concurrency`: Max sources to fetch from simultaneously

#### `[[sources]]` - Source Definitions
You can define multiple sources by repeating the `[[sources]]` section.

**Common fields:**
- `type`: Source type, currently only "reddit"
- `enabled`: Enable/disable this source

**Reddit-specific fields:**
- `subreddit`: Target subreddit (without /r/ prefix)
- `limit`: Number of posts to fetch (supports pagination for any limit)
- `sort_by`: Sorting method - "hot", "new", "top", or "rising"
- `time_filter`: Required for "top" sorting - "hour", "day", "week", "month", "year", "all"
- `min_score`: Filter posts below this score
- `min_comments`: Filter posts with fewer than this many comments
- `user_agent`: User-Agent for this source
- `rate_limit_delay_ms`: Delay between paginated requests (milliseconds)

## Usage

The new usage pattern uses **stdin for configuration** and **CLI arguments for queries**:

```bash
cat config.toml | cargo run -- --keywords <KEYWORDS>... [OPTIONS]
```

### Basic Examples

```bash
# Search for posts containing "async"
cat config.toml | cargo run -- --keywords "async"

# Search for posts containing "async" OR "tokio"
cat config.toml | cargo run -- --keywords "async" --keywords "tokio"

# Search for posts containing "rust" AND "async" (all keywords must match)
cat config.toml | cargo run -- --keywords "rust" --keywords "async" --match-mode all

# Output to file
cat config.toml | cargo run -- --keywords "tutorial" --output results.json

# Override log level
cat config.toml | cargo run -- --keywords "rust" --log-level debug

# Pipe output to jq for processing
cat config.toml | cargo run -- --keywords "rust" | jq '.[].title'
```

### CLI Options

- `--keywords <KEYWORD>`: Search keyword (can be specified multiple times, required)
- `--match-mode <MODE>`: Keyword matching mode - "any" (default) or "all"
- `-o, --output <PATH>`: Override output destination
- `--log-level <LEVEL>`: Override log level

### Keyword Matching

- **Case-insensitive**: "Rust", "rust", "RUST" all match
- **Substring matching**: "async" matches "asynchronous", "async/await"
- **Title and body**: Searches in both post title and body text
- **Match modes**:
  - `any` (default): Post matches if it contains ANY keyword (OR logic)
  - `all`: Post matches only if it contains ALL keywords (AND logic)

## Output Format

The crawler outputs a JSON array of posts:

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
    "source_type": "reddit",
    "source_id": "reddit:rust:hot"
  }
]
```

**Fields:**
- `source_type`: Type of source ("reddit", etc.)
- `source_id`: Unique identifier for the source instance (format: "reddit:subreddit:sort_by")

## Testing

Run unit tests:
```bash
cargo test
```

Run integration tests:
```bash
cargo test --test integration_test
```

## Architecture

```
src/
├── main.rs           - CLI argument parsing and orchestration
├── config.rs         - TOML configuration with tagged enum deserialization
├── source/
│   ├── mod.rs        - Source trait, filters, and factory
│   └── reddit.rs     - Reddit API client with pagination
└── output.rs         - JSON output formatting
```

### Source Abstraction

The `Source` trait provides a clean abstraction for content sources:

```rust
pub trait Source: Send + Sync {
    async fn fetch(&self, filters: &SourceFilters) -> Result<Vec<Content>>;
    fn source_type(&self) -> &str;
    fn source_id(&self) -> String;
}
```

### Adding New Sources

To add a new content source:

1. **Define config struct** in `config.rs`:
   ```rust
   #[derive(Deserialize)]
   pub struct MySourceConfig {
       // source-specific fields
   }
   ```

2. **Add enum variant** in `config.rs`:
   ```rust
   #[serde(tag = "type")]
   pub enum SourceConfig {
       Reddit(RedditConfig),
       MySource(MySourceConfig),  // Add this
   }
   ```

3. **Implement source** in `src/source/mysource.rs`:
   ```rust
   pub struct MySourceClient { /* ... */ }

   impl Source for MySourceClient {
       async fn fetch(&self, filters: &SourceFilters) -> Result<Vec<Content>> {
           // Fetch and filter content
       }

       fn source_type(&self) -> &str { "mysource" }
       fn source_id(&self) -> String { /* unique ID */ }
   }
   ```

4. **Update factory** in `src/source/mod.rs`:
   ```rust
   pub fn build_source(config: SourceConfig, client: Arc<Client>) -> Result<Box<dyn Source>> {
       match config {
           SourceConfig::Reddit(c) => Ok(Box::new(RedditClient::new(c, client)?)),
           SourceConfig::MySource(c) => Ok(Box::new(MySourceClient::new(c, client)?)),
       }
   }
   ```

No changes needed to `main.rs` - the factory pattern handles instantiation automatically!

## Implementation Details

### Reddit API

- Uses public JSON endpoints (no OAuth required)
- User-Agent header is required by Reddit API
- Respects rate limiting with configurable delays
- Pagination handled automatically via `after` tokens
- Supports multiple sorting methods and time filters

### Concurrency

- Multiple sources fetch concurrently up to `max_concurrency` limit
- Uses `futures::stream` with `.buffered()` for controlled concurrency
- Fail-fast behavior: any source error stops entire operation

### Configuration Parsing

- Uses serde's tagged enum feature for type-safe source configs
- Comprehensive validation at parse time
- Clear error messages for configuration issues

## Notes

- **Stdin-only**: Configuration must be piped via stdin (no file path argument)
- **Keywords required**: At least one keyword must be provided via CLI
- **User-Agent required**: Reddit API requires a descriptive User-Agent header
- **Rate limiting**: Configure `rate_limit_delay_ms` to avoid 429 errors
- **Stderr for logs**: Progress messages go to stderr, JSON output to stdout
- **Fail-fast**: Any source failure stops the entire crawl

## Future Enhancements

- OAuth authentication for higher Reddit rate limits
- Additional sources (Hacker News, Stack Overflow, etc.)
- BM25 ranking for relevance scoring
- Regex pattern matching for keywords
- Export to other formats (CSV, JSONL)
- Resume/checkpoint support for large crawls
