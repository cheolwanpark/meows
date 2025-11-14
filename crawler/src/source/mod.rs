use anyhow::Result;
use async_trait::async_trait;
use serde::{Deserialize, Serialize};
use std::sync::Arc;

// Re-export source implementations
pub mod reddit;

use crate::config::SourceConfig;
use reddit::RedditClient;

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
    pub source_type: String,  // "reddit", "hackernews", etc.
    pub source_id: String,    // Unique identifier for this source instance
}

/// Match mode for keyword filtering
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum MatchMode {
    /// Match if ANY keyword is found
    Any,
    /// Match only if ALL keywords are found
    All,
}

impl MatchMode {
    pub fn from_str(s: &str) -> Result<Self> {
        match s.to_lowercase().as_str() {
            "any" => Ok(MatchMode::Any),
            "all" => Ok(MatchMode::All),
            _ => anyhow::bail!("Invalid match mode: {}. Must be 'any' or 'all'", s),
        }
    }
}

/// Runtime filters that can be applied to any source
#[derive(Debug, Clone)]
pub struct SourceFilters {
    #[allow(dead_code)] // Kept public for API users to inspect original keywords
    pub keywords: Vec<String>,
    lowercase_keywords: Vec<String>, // Pre-lowercased for efficiency
    pub match_mode: MatchMode,
}

impl SourceFilters {
    pub fn new(keywords: Vec<String>, match_mode: MatchMode) -> Self {
        let lowercase_keywords = keywords.iter().map(|k| k.to_lowercase()).collect();
        Self {
            keywords,
            lowercase_keywords,
            match_mode,
        }
    }

    /// Check if content matches the keyword filters
    pub fn matches(&self, content: &Content) -> bool {
        if self.lowercase_keywords.is_empty() {
            return true;
        }

        let text = format!("{} {}", content.title, content.body).to_lowercase();

        match self.match_mode {
            MatchMode::Any => {
                self.lowercase_keywords.iter().any(|keyword| {
                    text.contains(keyword)
                })
            }
            MatchMode::All => {
                self.lowercase_keywords.iter().all(|keyword| {
                    text.contains(keyword)
                })
            }
        }
    }
}

/// Abstract trait for content sources
///
/// All content sources (Reddit, HackerNews, etc.) should implement this trait
/// to provide a unified interface for fetching content.
#[async_trait]
pub trait Source: Send + Sync {
    /// Fetch content from this source with given filters
    ///
    /// # Arguments
    /// * `filters` - Runtime filters to apply (keywords, match mode)
    ///
    /// # Returns
    /// A vector of Content items fetched from the source that match the filters
    ///
    /// # Errors
    /// Returns an error if the source is unreachable, rate-limited,
    /// or if the response cannot be parsed
    async fn fetch(&self, filters: &SourceFilters) -> Result<Vec<Content>>;

    /// Get the source type identifier
    #[allow(dead_code)]
    fn source_type(&self) -> &str;

    /// Get unique identifier for this source instance
    fn source_id(&self) -> String;
}

/// Factory function to build a Source from configuration
///
/// # Arguments
/// * `config` - The source configuration
/// * `client` - Shared HTTP client for connection pooling
///
/// # Returns
/// A boxed trait object implementing Source
pub fn build_source(
    config: SourceConfig,
    client: Arc<reqwest::Client>,
) -> Result<Box<dyn Source>> {
    match config {
        SourceConfig::Reddit(reddit_config) => {
            Ok(Box::new(RedditClient::new(reddit_config, client)?))
        }
    }
}
