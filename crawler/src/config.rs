use anyhow::{bail, Context, Result};
use serde::{Deserialize, Serialize};
use std::io::Read;

#[derive(Debug, Deserialize, Serialize, Clone)]
pub struct Config {
    pub crawler: CrawlerConfig,
    pub sources: Vec<SourceEntry>,
}

#[derive(Debug, Deserialize, Serialize, Clone)]
pub struct CrawlerConfig {
    #[serde(default = "default_output_format")]
    pub output_format: String,

    #[serde(default = "default_output_destination")]
    pub output_destination: String,

    #[serde(default = "default_log_level")]
    pub log_level: String,

    #[serde(default = "default_max_concurrency")]
    pub max_concurrency: usize,

    pub user_agent: String,
}

#[derive(Debug, Deserialize, Serialize, Clone)]
pub struct SourceEntry {
    #[serde(default = "default_enabled")]
    pub enabled: bool,

    #[serde(flatten)]
    pub config: SourceConfig,
}

#[derive(Debug, Deserialize, Serialize, Clone)]
#[serde(tag = "type", rename_all = "lowercase")]
pub enum SourceConfig {
    Reddit(RedditConfig),
    SemanticScholar(SemanticScholarConfig),
}

#[derive(Debug, Deserialize, Serialize, Clone)]
pub struct RedditConfig {
    pub subreddit: String,

    #[serde(default = "default_limit")]
    pub limit: usize,

    #[serde(default = "default_sort_by")]
    pub sort_by: String,

    pub time_filter: Option<String>,

    #[serde(default)]
    pub min_score: i32,

    #[serde(default)]
    pub min_comments: i32,

    #[serde(default = "default_user_agent")]
    pub user_agent: String,

    #[serde(default = "default_rate_limit_delay_ms")]
    pub rate_limit_delay_ms: u64,
}

#[derive(Debug, Deserialize, Serialize, Clone)]
#[serde(tag = "mode", rename_all = "lowercase")]
pub enum SemanticScholarMode {
    Search {
        query: String,
        #[serde(default)]
        year: Option<String>,
    },
    Recommendations {
        paper_id: String,
    },
}

#[derive(Debug, Deserialize, Serialize, Clone)]
pub struct SemanticScholarConfig {
    #[serde(flatten)]
    pub mode: SemanticScholarMode,

    #[serde(default = "default_max_results")]
    pub max_results: usize,

    #[serde(default)]
    pub min_citations: i32,

    pub api_key: Option<String>,

    #[serde(default = "default_rate_limit_delay_ms")]
    pub rate_limit_delay_ms: u64,
}

// Default value functions
fn default_output_format() -> String {
    "json".to_string()
}

fn default_output_destination() -> String {
    "stdout".to_string()
}

fn default_log_level() -> String {
    "info".to_string()
}

fn default_max_concurrency() -> usize {
    5
}

fn default_enabled() -> bool {
    true
}

fn default_limit() -> usize {
    100
}

fn default_sort_by() -> String {
    "hot".to_string()
}

fn default_user_agent() -> String {
    "crawler/0.1.0".to_string()
}

fn default_rate_limit_delay_ms() -> u64 {
    1000
}

fn default_max_results() -> usize {
    100
}

impl Config {
    /// Load configuration from stdin
    pub fn from_stdin() -> Result<Self> {
        let mut buffer = String::new();
        std::io::stdin()
            .read_to_string(&mut buffer)
            .context("Failed to read configuration from stdin")?;

        Self::from_str(&buffer)
    }

    /// Load configuration from a string (useful for testing)
    pub fn from_str(contents: &str) -> Result<Self> {
        let config: Config =
            toml::from_str(contents).context("Failed to parse TOML configuration")?;

        config.validate()?;
        Ok(config)
    }

    /// Validate configuration values
    fn validate(&self) -> Result<()> {
        // Validate crawler config
        if self.crawler.user_agent.is_empty() {
            bail!("crawler.user_agent cannot be empty (required by Reddit API)");
        }

        if self.crawler.max_concurrency == 0 {
            bail!("crawler.max_concurrency must be greater than 0");
        }

        let valid_log_levels = ["error", "warn", "info", "debug", "trace"];
        if !valid_log_levels.contains(&self.crawler.log_level.as_str()) {
            bail!(
                "crawler.log_level must be one of: {:?}, got: {}",
                valid_log_levels,
                self.crawler.log_level
            );
        }

        // Validate sources
        if self.sources.is_empty() {
            bail!("At least one source must be configured");
        }

        let enabled_count = self.sources.iter().filter(|s| s.enabled).count();
        if enabled_count == 0 {
            bail!("At least one source must be enabled");
        }

        // Validate each source
        for (idx, source) in self.sources.iter().enumerate() {
            match &source.config {
                SourceConfig::Reddit(reddit_config) => {
                    self.validate_reddit_config(reddit_config, idx)?;
                }
                SourceConfig::SemanticScholar(semantic_scholar_config) => {
                    self.validate_semantic_scholar_config(semantic_scholar_config, idx)?;
                }
            }
        }

        Ok(())
    }

    fn validate_reddit_config(&self, config: &RedditConfig, idx: usize) -> Result<()> {
        // Validate subreddit name
        if config.subreddit.is_empty() {
            bail!("sources[{}]: subreddit cannot be empty", idx);
        }

        if config.subreddit.starts_with("/r/") || config.subreddit.starts_with("r/") {
            bail!(
                "sources[{}]: subreddit should not include '/r/' prefix, got: {}",
                idx,
                config.subreddit
            );
        }

        // Validate limit (no upper bound due to pagination support)
        if config.limit == 0 {
            bail!("sources[{}]: limit must be greater than 0", idx);
        }

        // Validate sort_by
        let valid_sort = ["hot", "new", "top", "rising"];
        if !valid_sort.contains(&config.sort_by.as_str()) {
            bail!(
                "sources[{}]: sort_by must be one of: {:?}, got: {}",
                idx,
                valid_sort,
                config.sort_by
            );
        }

        // Validate time_filter is provided when sort_by is "top"
        if config.sort_by == "top" && config.time_filter.is_none() {
            bail!(
                "sources[{}]: time_filter is required when sort_by is 'top'",
                idx
            );
        }

        // Validate time_filter values if provided
        if let Some(ref time_filter) = config.time_filter {
            let valid_filters = ["hour", "day", "week", "month", "year", "all"];
            if !valid_filters.contains(&time_filter.as_str()) {
                bail!(
                    "sources[{}]: time_filter must be one of: {:?}, got: {}",
                    idx,
                    valid_filters,
                    time_filter
                );
            }
        }

        // Validate user_agent
        if config.user_agent.is_empty() {
            bail!(
                "sources[{}]: user_agent cannot be empty (required by Reddit API)",
                idx
            );
        }

        Ok(())
    }

    fn validate_semantic_scholar_config(
        &self,
        config: &SemanticScholarConfig,
        idx: usize,
    ) -> Result<()> {
        // Validate max_results
        if config.max_results == 0 {
            bail!("sources[{}]: max_results must be greater than 0", idx);
        }

        // Validate min_citations (must be non-negative)
        if config.min_citations < 0 {
            bail!(
                "sources[{}]: min_citations must be non-negative, got: {}",
                idx,
                config.min_citations
            );
        }

        // Validate year format if provided in search mode
        match &config.mode {
            SemanticScholarMode::Search { query, year } => {
                if query.is_empty() {
                    bail!("sources[{}]: query cannot be empty in search mode", idx);
                }

                // Validate year format if provided
                if let Some(year_str) = year {
                    self.validate_year_format(year_str, idx)?;
                }
            }
            SemanticScholarMode::Recommendations { paper_id } => {
                if paper_id.is_empty() {
                    bail!(
                        "sources[{}]: paper_id cannot be empty in recommendations mode",
                        idx
                    );
                }
            }
        }

        Ok(())
    }

    fn validate_year_format(&self, year: &str, idx: usize) -> Result<()> {
        // Valid formats: "2020", "2020-2024", "2020-", "-2024"
        if year.is_empty() {
            bail!("sources[{}]: year cannot be empty", idx);
        }

        if year.contains('-') {
            let parts: Vec<&str> = year.split('-').collect();
            if parts.len() != 2 {
                bail!(
                    "sources[{}]: invalid year range format '{}', expected 'YYYY-YYYY', 'YYYY-', or '-YYYY'",
                    idx,
                    year
                );
            }

            // Validate start year if provided
            if !parts[0].is_empty() && parts[0].parse::<i32>().is_err() {
                bail!(
                    "sources[{}]: invalid start year '{}' in range '{}'",
                    idx,
                    parts[0],
                    year
                );
            }

            // Validate end year if provided
            if !parts[1].is_empty() && parts[1].parse::<i32>().is_err() {
                bail!(
                    "sources[{}]: invalid end year '{}' in range '{}'",
                    idx,
                    parts[1],
                    year
                );
            }
        } else {
            // Single year
            if year.parse::<i32>().is_err() {
                bail!(
                    "sources[{}]: invalid year '{}', must be a valid integer",
                    idx,
                    year
                );
            }
        }

        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_valid_config() {
        let toml = r#"
            [crawler]
            output_format = "json"
            output_destination = "stdout"
            log_level = "info"
            max_concurrency = 5
            user_agent = "test-crawler/1.0"

            [[sources]]
            type = "reddit"
            enabled = true
            subreddit = "rust"
            limit = 100
            sort_by = "hot"
            min_score = 0
            min_comments = 0
            user_agent = "test-crawler/1.0"
            rate_limit_delay_ms = 1000
        "#;

        let config = Config::from_str(toml).unwrap();
        assert_eq!(config.crawler.user_agent, "test-crawler/1.0");
        assert_eq!(config.sources.len(), 1);
        assert!(config.sources[0].enabled);
    }

    #[test]
    fn test_multiple_sources() {
        let toml = r#"
            [crawler]
            user_agent = "test-crawler/1.0"

            [[sources]]
            type = "reddit"
            enabled = true
            subreddit = "rust"
            user_agent = "test-crawler/1.0"

            [[sources]]
            type = "reddit"
            enabled = false
            subreddit = "programming"
            user_agent = "test-crawler/1.0"
        "#;

        let config = Config::from_str(toml).unwrap();
        assert_eq!(config.sources.len(), 2);
        assert!(config.sources[0].enabled);
        assert!(!config.sources[1].enabled);
    }

    #[test]
    fn test_top_requires_time_filter() {
        let toml = r#"
            [crawler]
            user_agent = "test-crawler/1.0"

            [[sources]]
            type = "reddit"
            subreddit = "rust"
            sort_by = "top"
            user_agent = "test-crawler/1.0"
        "#;

        let result = Config::from_str(toml);
        assert!(result.is_err());
        assert!(result
            .unwrap_err()
            .to_string()
            .contains("time_filter is required"));
    }

    #[test]
    fn test_top_with_time_filter() {
        let toml = r#"
            [crawler]
            user_agent = "test-crawler/1.0"

            [[sources]]
            type = "reddit"
            subreddit = "rust"
            sort_by = "top"
            time_filter = "day"
            user_agent = "test-crawler/1.0"
        "#;

        let config = Config::from_str(toml).unwrap();
        if let SourceConfig::Reddit(ref reddit) = config.sources[0].config {
            assert_eq!(reddit.sort_by, "top");
            assert_eq!(reddit.time_filter, Some("day".to_string()));
        }
    }

    #[test]
    fn test_invalid_sort_by() {
        let toml = r#"
            [crawler]
            user_agent = "test-crawler/1.0"

            [[sources]]
            type = "reddit"
            subreddit = "rust"
            sort_by = "invalid"
            user_agent = "test-crawler/1.0"
        "#;

        let result = Config::from_str(toml);
        assert!(result.is_err());
    }

    #[test]
    fn test_empty_user_agent() {
        let toml = r#"
            [crawler]
            user_agent = ""

            [[sources]]
            type = "reddit"
            subreddit = "rust"
            user_agent = "test-crawler/1.0"
        "#;

        let result = Config::from_str(toml);
        assert!(result.is_err());
        assert!(result
            .unwrap_err()
            .to_string()
            .contains("user_agent cannot be empty"));
    }

    #[test]
    fn test_semantic_scholar_search_config() {
        let toml = r#"
            [crawler]
            user_agent = "test-crawler/1.0"

            [[sources]]
            type = "semanticscholar"
            enabled = true
            mode = "search"
            query = "machine learning"
            year = "2020-2024"
            max_results = 50
            min_citations = 10
            rate_limit_delay_ms = 1000
        "#;

        let config = Config::from_str(toml).unwrap();
        assert_eq!(config.sources.len(), 1);

        match &config.sources[0].config {
            SourceConfig::SemanticScholar(s2) => {
                assert_eq!(s2.max_results, 50);
                assert_eq!(s2.min_citations, 10);
                match &s2.mode {
                    SemanticScholarMode::Search { query, year } => {
                        assert_eq!(query, "machine learning");
                        assert_eq!(year.as_deref(), Some("2020-2024"));
                    }
                    _ => panic!("Expected Search mode"),
                }
            }
            _ => panic!("Expected SemanticScholar config"),
        }
    }

    #[test]
    fn test_semantic_scholar_recommendations_config() {
        let toml = r#"
            [crawler]
            user_agent = "test-crawler/1.0"

            [[sources]]
            type = "semanticscholar"
            enabled = true
            mode = "recommendations"
            paper_id = "abc123"
            max_results = 20
            min_citations = 5
        "#;

        let config = Config::from_str(toml).unwrap();
        match &config.sources[0].config {
            SourceConfig::SemanticScholar(s2) => match &s2.mode {
                SemanticScholarMode::Recommendations { paper_id } => {
                    assert_eq!(paper_id, "abc123");
                }
                _ => panic!("Expected Recommendations mode"),
            },
            _ => panic!("Expected SemanticScholar config"),
        }
    }

    #[test]
    fn test_no_enabled_sources() {
        let toml = r#"
            [crawler]
            user_agent = "test-crawler/1.0"

            [[sources]]
            type = "reddit"
            enabled = false
            subreddit = "rust"
            user_agent = "test-crawler/1.0"
        "#;

        let result = Config::from_str(toml);
        assert!(result.is_err());
        assert!(result
            .unwrap_err()
            .to_string()
            .contains("At least one source must be enabled"));
    }
}
