use crate::source::{Content, Source};
use anyhow::{Context, Result};
use serde::Deserialize;
use std::time::Duration;

/// Reddit API JSON response structure
/// Docs: https://www.reddit.com/dev/api/#GET_hot
#[derive(Debug, Deserialize)]
struct RedditResponse {
    kind: String,  // "Listing" for post listings
    data: RedditData,
}

#[derive(Debug, Deserialize)]
struct RedditData {
    children: Vec<RedditChild>,
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
    subreddit: String,
}

/// Reddit API client
pub struct RedditClient {
    client: reqwest::Client,
}

impl RedditClient {
    /// Create a new Reddit client with custom User-Agent
    pub fn new() -> Result<Self> {
        let client = reqwest::Client::builder()
            .user_agent("crawler/0.1.0 (by /u/crawler_bot)")
            .timeout(Duration::from_secs(30))
            .build()
            .context("Failed to build HTTP client")?;

        Ok(Self { client })
    }

    /// Fetch hot posts from a subreddit
    ///
    /// # Arguments
    /// * `subreddit` - Name of subreddit (without /r/ prefix)
    /// * `limit` - Maximum number of posts to fetch (1-100)
    ///
    /// # Example
    /// ```no_run
    /// let client = RedditClient::new()?;
    /// let posts = client.fetch_hot("rust", 50).await?;
    /// ```
    pub async fn fetch_hot(&self, subreddit: &str, limit: usize) -> Result<Vec<Content>> {
        let url = format!(
            "https://www.reddit.com/r/{}/hot.json?limit={}&raw_json=1",
            subreddit, limit
        );

        let response = self
            .client
            .get(&url)
            .send()
            .await
            .context(format!("Failed to fetch from /r/{}", subreddit))?;

        // Check for rate limiting
        if response.status() == 429 {
            anyhow::bail!(
                "Rate limited by Reddit API. Status: 429 Too Many Requests. \
                Please wait a few minutes before trying again."
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

        // Convert Reddit posts to common Content structure
        let contents: Vec<Content> = reddit_response
            .data
            .children
            .into_iter()
            .map(|child| {
                let post = child.data;
                Content {
                    id: post.id,
                    title: post.title,
                    body: post.selftext,
                    url: post.url,
                    author: post.author,
                    created_utc: post.created_utc as i64,
                    score: post.score,
                    num_comments: post.num_comments,
                    source: format!("reddit:{}", post.subreddit),
                }
            })
            .collect();

        Ok(contents)
    }
}

impl Source for RedditClient {
    async fn fetch(&self) -> Result<Vec<Content>> {
        // Default to fetching 25 posts from "rust" subreddit
        // In real usage, these would come from config
        self.fetch_hot("rust", 25).await
    }

    fn name(&self) -> &str {
        "reddit"
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_reddit_response_deserialization() {
        let json = r#"
        {
            "kind": "Listing",
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
                ]
            }
        }
        "#;

        let response: RedditResponse = serde_json::from_str(json).unwrap();
        assert_eq!(response.kind, "Listing");
        assert_eq!(response.data.children.len(), 1);
        assert_eq!(response.data.children[0].data.title, "Test Post");
    }
}
