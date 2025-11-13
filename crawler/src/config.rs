use anyhow::{bail, Context, Result};
use serde::{Deserialize, Serialize};
use std::path::Path;

#[derive(Debug, Deserialize, Serialize, Clone)]
pub struct Config {
    pub output: OutputConfig,
    pub filter: FilterConfig,
    pub sources: SourcesConfig,
}

#[derive(Debug, Deserialize, Serialize, Clone)]
pub struct OutputConfig {
    pub destination: String, // "stdout" or file path
}

#[derive(Debug, Deserialize, Serialize, Clone)]
pub struct FilterConfig {
    pub keywords: Vec<String>,
    #[serde(default = "default_match_mode")]
    pub match_mode: String, // "any" or "all"
}

fn default_match_mode() -> String {
    "any".to_string()
}

#[derive(Debug, Deserialize, Serialize, Clone)]
pub struct SourcesConfig {
    pub reddit: RedditConfig,
}

#[derive(Debug, Deserialize, Serialize, Clone)]
pub struct RedditConfig {
    pub subreddit: String,
    #[serde(default = "default_limit")]
    pub limit: usize,
    #[serde(default = "default_user_agent")]
    pub user_agent: String,
}

fn default_limit() -> usize {
    100
}

fn default_user_agent() -> String {
    "crawler/0.1.0 (by /u/crawler_bot)".to_string()
}

impl Config {
    /// Load configuration from a TOML file
    pub fn from_file(path: &str) -> Result<Self> {
        let contents = std::fs::read_to_string(path)
            .context(format!("Failed to read config file: {}", path))?;

        let config: Config =
            toml::from_str(&contents).context("Failed to parse TOML configuration")?;

        config.validate()?;
        Ok(config)
    }

    /// Validate configuration values
    fn validate(&self) -> Result<()> {
        // Validate keywords
        if self.filter.keywords.is_empty() {
            bail!("filter.keywords cannot be empty");
        }

        // Validate match mode
        if self.filter.match_mode != "any" && self.filter.match_mode != "all" {
            bail!(
                "filter.match_mode must be 'any' or 'all', got: {}",
                self.filter.match_mode
            );
        }

        // Validate subreddit name
        if self.sources.reddit.subreddit.is_empty() {
            bail!("sources.reddit.subreddit cannot be empty");
        }

        if self.sources.reddit.subreddit.starts_with("/r/")
            || self.sources.reddit.subreddit.starts_with("r/")
        {
            bail!("sources.reddit.subreddit should not include '/r/' prefix");
        }

        // Validate limit
        if self.sources.reddit.limit == 0 || self.sources.reddit.limit > 100 {
            bail!("sources.reddit.limit must be between 1 and 100");
        }

        // Validate output destination if it's a file path
        if self.output.destination != "stdout" {
            let path = Path::new(&self.output.destination);
            if let Some(parent) = path.parent() {
                if !parent.as_os_str().is_empty() && !parent.exists() {
                    bail!("Output directory does not exist: {}", parent.display());
                }
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
            [output]
            destination = "stdout"

            [filter]
            keywords = ["rust", "async"]
            match_mode = "any"

            [sources.reddit]
            subreddit = "rust"
            limit = 50
        "#;

        let config: Config = toml::from_str(toml).unwrap();
        assert!(config.validate().is_ok());
    }

    #[test]
    fn test_empty_keywords() {
        let toml = r#"
            [output]
            destination = "stdout"

            [filter]
            keywords = []

            [sources.reddit]
            subreddit = "rust"
        "#;

        let config: Config = toml::from_str(toml).unwrap();
        assert!(config.validate().is_err());
    }

    #[test]
    fn test_invalid_match_mode() {
        let toml = r#"
            [output]
            destination = "stdout"

            [filter]
            keywords = ["rust"]
            match_mode = "invalid"

            [sources.reddit]
            subreddit = "rust"
        "#;

        let config: Config = toml::from_str(toml).unwrap();
        assert!(config.validate().is_err());
    }
}
