# API Examples

## Create Reddit Source

```bash
curl -X POST http://localhost:8080/sources \
  -H "Content-Type: application/json" \
  -d '{
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
  }'
```

## Create Semantic Scholar Source (Search)

```bash
curl -X POST http://localhost:8080/sources \
  -H "Content-Type: application/json" \
  -d '{
    "type": "semantic_scholar",
    "cron_expr": "0 0 * * *",
    "config": {
      "mode": "search",
      "query": "large language models",
      "year": "2024",
      "max_results": 50,
      "min_citations": 10,
      "rate_limit_delay_ms": 1000
    }
  }'
```

## Create Semantic Scholar Source (Recommendations)

```bash
curl -X POST http://localhost:8080/sources \
  -H "Content-Type: application/json" \
  -d '{
    "type": "semantic_scholar",
    "cron_expr": "0 12 * * *",
    "config": {
      "mode": "recommendations",
      "paper_id": "649def34f8be52c8b66281af98ae884c09aef38b",
      "max_results": 20,
      "min_citations": 5,
      "rate_limit_delay_ms": 1000
    }
  }'
```

## List All Sources

```bash
curl http://localhost:8080/sources
```

## List Reddit Sources Only

```bash
curl http://localhost:8080/sources?type=reddit
```

## Get Specific Source

```bash
curl http://localhost:8080/sources/{SOURCE_ID}
```

## Update Source Schedule

```bash
curl -X PUT http://localhost:8080/sources/{SOURCE_ID} \
  -H "Content-Type: application/json" \
  -d '{
    "cron_expr": "0 */12 * * *"
  }'
```

## Delete Source

```bash
curl -X DELETE http://localhost:8080/sources/{SOURCE_ID}
```

## Get 24h Schedule

```bash
curl http://localhost:8080/schedule
```

## List Articles

```bash
# All articles
curl http://localhost:8080/articles

# With pagination
curl "http://localhost:8080/articles?limit=20&offset=0"

# Filter by source
curl "http://localhost:8080/articles?source_id={SOURCE_ID}"

# Filter by date
curl "http://localhost:8080/articles?since=2024-11-15T00:00:00Z"

# Combined filters
curl "http://localhost:8080/articles?source_id={SOURCE_ID}&limit=50&since=2024-11-01T00:00:00Z"
```

## Health Check

```bash
curl http://localhost:8080/health
```

## Metrics

```bash
curl http://localhost:8080/metrics
```

## Test Workflow

1. Start the server:
```bash
./bin/collector
```

2. Add a Reddit source:
```bash
curl -X POST http://localhost:8080/sources \
  -H "Content-Type: application/json" \
  -d '{
    "type": "reddit",
    "cron_expr": "*/5 * * * *",
    "config": {
      "subreddit": "golang",
      "sort": "hot",
      "limit": 10,
      "min_score": 0,
      "min_comments": 0,
      "user_agent": "meows-collector/1.0 test",
      "rate_limit_delay_ms": 2000
    }
  }'
```

3. Check the schedule:
```bash
curl http://localhost:8080/schedule
```

4. Wait 5 minutes or trigger manually by restarting with a past cron time

5. Query articles:
```bash
curl http://localhost:8080/articles
```

6. Check metrics:
```bash
curl http://localhost:8080/metrics
```
