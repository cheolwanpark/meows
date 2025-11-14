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
