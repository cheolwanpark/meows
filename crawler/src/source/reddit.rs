use crate::config::RedditConfig;
use crate::source::{Content, Source, SourceFilters};
use anyhow::{Context, Result};
use async_trait::async_trait;
use serde::Deserialize;
use std::sync::Arc;
use std::time::Duration;
use tokio::time::sleep;

/// Reddit API JSON response structure
/// Docs: https://www.reddit.com/dev/api/#GET_hot
#[derive(Debug, Deserialize)]
struct RedditResponse {
    data: RedditData,
}

#[derive(Debug, Deserialize)]
struct RedditData {
    children: Vec<RedditChild>,
    after: Option<String>,  // Pagination token
}

#[derive(Debug, Deserialize)]
struct RedditChild {
    data: RedditPost,
}

#[derive(Debug, Deserialize)]
struct RedditPost {
    id: String,
    title: String,
    #[serde(default)]
    selftext: String,  // Empty for link posts
    url: Option<String>,
    author: String,
    created_utc: f64,  // Unix timestamp
    score: i32,
    num_comments: i32,
    #[allow(dead_code)]
    subreddit: String,
}

/// Reddit API client
pub struct RedditClient {
    client: Arc<reqwest::Client>,
    config: RedditConfig,
}

impl RedditClient {
    /// Create a new Reddit client with configuration
    ///
    /// # Arguments
    /// * `config` - Reddit-specific configuration
    /// * `client` - Shared HTTP client for connection pooling
    pub fn new(config: RedditConfig, client: Arc<reqwest::Client>) -> Result<Self> {
        // Basic validation (detailed validation done in config.rs)
        if config.subreddit.is_empty() {
            anyhow::bail!("subreddit cannot be empty");
        }
        if config.user_agent.is_empty() {
            anyhow::bail!("user_agent cannot be empty");
        }

        Ok(Self { client, config })
    }

    /// Build URL for the specified sort type
    fn build_url(&self, after: Option<&str>) -> String {
        let base_url = match self.config.sort_by.as_str() {
            "hot" => format!("https://www.reddit.com/r/{}/hot.json", self.config.subreddit),
            "new" => format!("https://www.reddit.com/r/{}/new.json", self.config.subreddit),
            "rising" => format!("https://www.reddit.com/r/{}/rising.json", self.config.subreddit),
            "top" => {
                let time_filter = self.config.time_filter.as_ref()
                    .map(|t| t.as_str())
                    .unwrap_or("day");
                format!(
                    "https://www.reddit.com/r/{}/top.json?t={}",
                    self.config.subreddit,
                    time_filter
                )
            }
            _ => format!("https://www.reddit.com/r/{}/hot.json", self.config.subreddit),
        };

        // Add pagination and limit
        let separator = if base_url.contains('?') { "&" } else { "?" };
        let mut url = format!("{}{}limit=100&raw_json=1", base_url, separator);

        if let Some(after_token) = after {
            url.push_str(&format!("&after={}", after_token));
        }

        url
    }

    /// Fetch posts from Reddit with pagination
    ///
    /// This method handles pagination automatically, making multiple requests
    /// if necessary to reach the configured limit.
    async fn fetch_posts(&self) -> Result<Vec<Content>> {
        let mut all_contents = Vec::new();
        let mut after: Option<String> = None;
        let target_limit = self.config.limit;

        loop {
            // Build URL with pagination token
            let url = self.build_url(after.as_deref());

            // Make request
            let response = self
                .client
                .get(&url)
                .header("User-Agent", &self.config.user_agent)
                .send()
                .await
                .context(format!("Failed to fetch from /r/{}", self.config.subreddit))?;

            // Check for rate limiting
            if response.status() == 429 {
                anyhow::bail!(
                    "Rate limited by Reddit API. Status: 429 Too Many Requests. \
                    Please wait before trying again."
                );
            }

            // Check for other errors
            if !response.status().is_success() {
                anyhow::bail!(
                    "Reddit API returned error: {} - {}",
                    response.status(),
                    response.status().canonical_reason().unwrap_or("Unknown")
                );
            }

            let reddit_response: RedditResponse = response
                .json()
                .await
                .context("Failed to parse Reddit JSON response")?;

            // Convert Reddit posts to Content
            let mut contents: Vec<Content> = reddit_response
                .data
                .children
                .into_iter()
                .map(|child| {
                    let post = child.data;
                    Content {
                        id: post.id.clone(),
                        title: post.title,
                        body: post.selftext,
                        url: post.url,
                        author: post.author,
                        created_utc: post.created_utc as i64,
                        score: post.score,
                        num_comments: post.num_comments,
                        source_type: "reddit".to_string(),
                        source_id: format!("reddit:{}:{}", self.config.subreddit, self.config.sort_by),
                    }
                })
                .collect();

            // Apply config-level filters
            contents = self.apply_config_filters(contents);

            all_contents.append(&mut contents);

            // Check if we've reached the target limit
            if all_contents.len() >= target_limit {
                all_contents.truncate(target_limit);
                break;
            }

            // Check if there's more data to fetch
            if let Some(after_token) = reddit_response.data.after {
                after = Some(after_token);

                // Rate limiting - sleep before next request
                sleep(Duration::from_millis(self.config.rate_limit_delay_ms)).await;
            } else {
                // No more data available
                break;
            }
        }

        Ok(all_contents)
    }

    /// Apply configuration-level filters (min_score, min_comments)
    fn apply_config_filters(&self, contents: Vec<Content>) -> Vec<Content> {
        contents
            .into_iter()
            .filter(|content| {
                content.score >= self.config.min_score
                    && content.num_comments >= self.config.min_comments
            })
            .collect()
    }
}

#[async_trait]
impl Source for RedditClient {
    async fn fetch(&self, filters: &SourceFilters) -> Result<Vec<Content>> {
        // Fetch posts from Reddit
        let mut contents = self.fetch_posts().await?;

        // Apply keyword filters
        contents.retain(|content| filters.matches(content));

        Ok(contents)
    }

    fn source_type(&self) -> &str {
        "reddit"
    }

    fn source_id(&self) -> String {
        format!("reddit:{}:{}", self.config.subreddit, self.config.sort_by)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::source::MatchMode;

    #[test]
    fn test_reddit_response_deserialization() {
        let json = r#"
        {
            "data": {
                "children": [
                    {
                        "data": {
                            "id": "abc123",
                            "title": "Test Post",
                            "selftext": "This is a test",
                            "url": "https://example.com",
                            "author": "testuser",
                            "created_utc": 1234567890.0,
                            "score": 100,
                            "num_comments": 10,
                            "subreddit": "test"
                        }
                    }
                ],
                "after": "t3_abc123"
            }
        }
        "#;

        let response: RedditResponse = serde_json::from_str(json).unwrap();
        assert_eq!(response.data.children.len(), 1);
        assert_eq!(response.data.children[0].data.title, "Test Post");
        assert_eq!(response.data.after, Some("t3_abc123".to_string()));
    }

    #[test]
    fn test_config_filters() {
        let config = RedditConfig {
            subreddit: "rust".to_string(),
            limit: 100,
            sort_by: "hot".to_string(),
            time_filter: None,
            min_score: 50,
            min_comments: 5,
            user_agent: "test/1.0".to_string(),
            rate_limit_delay_ms: 1000,
        };

        let client = Arc::new(reqwest::Client::new());
        let reddit_client = RedditClient::new(config, client).unwrap();

        let contents = vec![
            Content {
                id: "1".to_string(),
                title: "High score".to_string(),
                body: "".to_string(),
                url: None,
                author: "user1".to_string(),
                created_utc: 0,
                score: 100,
                num_comments: 10,
                source_type: "reddit".to_string(),
                source_id: "reddit:rust:hot".to_string(),
            },
            Content {
                id: "2".to_string(),
                title: "Low score".to_string(),
                body: "".to_string(),
                url: None,
                author: "user2".to_string(),
                created_utc: 0,
                score: 10,  // Below min_score
                num_comments: 10,
                source_type: "reddit".to_string(),
                source_id: "reddit:rust:hot".to_string(),
            },
            Content {
                id: "3".to_string(),
                title: "Low comments".to_string(),
                body: "".to_string(),
                url: None,
                author: "user3".to_string(),
                created_utc: 0,
                score: 100,
                num_comments: 2,  // Below min_comments
                source_type: "reddit".to_string(),
                source_id: "reddit:rust:hot".to_string(),
            },
        ];

        let filtered = reddit_client.apply_config_filters(contents);
        assert_eq!(filtered.len(), 1);
        assert_eq!(filtered[0].id, "1");
    }

    #[test]
    fn test_keyword_filter_integration() {
        let filters = SourceFilters::new(
            vec!["rust".to_string(), "async".to_string()],
            MatchMode::Any,
        );

        let content1 = Content {
            id: "1".to_string(),
            title: "Learning Rust".to_string(),
            body: "Great language".to_string(),
            url: None,
            author: "user1".to_string(),
            created_utc: 0,
            score: 100,
            num_comments: 10,
            source_type: "reddit".to_string(),
            source_id: "reddit:rust:hot".to_string(),
        };

        let content2 = Content {
            id: "2".to_string(),
            title: "Python tutorial".to_string(),
            body: "No match here".to_string(),
            url: None,
            author: "user2".to_string(),
            created_utc: 0,
            score: 100,
            num_comments: 10,
            source_type: "reddit".to_string(),
            source_id: "reddit:python:hot".to_string(),
        };

        assert!(filters.matches(&content1));
        assert!(!filters.matches(&content2));
    }

    #[test]
    fn test_url_building() {
        let client = Arc::new(reqwest::Client::new());

        // Test hot
        let config_hot = RedditConfig {
            subreddit: "rust".to_string(),
            limit: 100,
            sort_by: "hot".to_string(),
            time_filter: None,
            min_score: 0,
            min_comments: 0,
            user_agent: "test/1.0".to_string(),
            rate_limit_delay_ms: 1000,
        };
        let reddit_hot = RedditClient::new(config_hot, client.clone()).unwrap();
        let url = reddit_hot.build_url(None);
        assert!(url.contains("/r/rust/hot.json"));
        assert!(url.contains("limit=100"));

        // Test top with time filter
        let config_top = RedditConfig {
            subreddit: "programming".to_string(),
            limit: 50,
            sort_by: "top".to_string(),
            time_filter: Some("week".to_string()),
            min_score: 0,
            min_comments: 0,
            user_agent: "test/1.0".to_string(),
            rate_limit_delay_ms: 1000,
        };
        let reddit_top = RedditClient::new(config_top, client.clone()).unwrap();
        let url = reddit_top.build_url(None);
        assert!(url.contains("/r/programming/top.json"));
        assert!(url.contains("t=week"));

        // Test pagination
        let url_with_after = reddit_hot.build_url(Some("t3_abc123"));
        assert!(url_with_after.contains("after=t3_abc123"));
    }
}
