use anyhow::Result;
use serde::{Deserialize, Serialize};

// Re-export source implementations
pub mod reddit;

/// Common content structure for crawled data across all sources
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Content {
    pub id: String,
    pub title: String,
    pub body: String,
    pub url: Option<String>,
    pub author: String,
    pub created_utc: i64,
    pub score: i32,
    pub num_comments: i32,
    pub source: String, // "reddit", "hackernews", etc.
}

/// Abstract trait for content sources
///
/// All content sources (Reddit, HackerNews, etc.) should implement this trait
/// to provide a unified interface for fetching content.
pub trait Source {
    /// Fetch content from this source
    ///
    /// # Returns
    /// A vector of Content items fetched from the source
    ///
    /// # Errors
    /// Returns an error if the source is unreachable, rate-limited,
    /// or if the response cannot be parsed
    fn fetch(&self) -> impl std::future::Future<Output = Result<Vec<Content>>> + Send;

    /// Get the name of this source (for logging and identification)
    #[allow(dead_code)]
    fn name(&self) -> &str;
}
