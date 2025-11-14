mod config;
mod output;
mod source;

use anyhow::{Context, Result};
use clap::Parser;
use futures::stream::{self, StreamExt, TryStreamExt};
use source::{build_source, MatchMode, Source, SourceFilters};
use std::sync::Arc;

#[derive(Parser)]
#[command(name = "crawler")]
#[command(version = "0.1.0")]
#[command(about = "Minimal content crawler with keyword filtering", long_about = None)]
struct Cli {
    /// Search keywords (can be specified multiple times)
    #[arg(long, required = true)]
    keywords: Vec<String>,

    /// Keyword match mode: 'any' or 'all'
    #[arg(long, default_value = "any")]
    match_mode: String,

    /// Override output destination (stdout or file path)
    #[arg(short, long)]
    output: Option<String>,

    /// Override log level (error, warn, info, debug, trace)
    #[arg(long)]
    log_level: Option<String>,
}

#[tokio::main]
async fn main() -> Result<()> {
    // Parse CLI arguments
    let cli = Cli::parse();

    // Load configuration from stdin
    let mut config = config::Config::from_stdin()
        .context("Failed to load configuration from stdin")?;

    // Apply CLI overrides with validation
    if let Some(output) = cli.output {
        config.crawler.output_destination = output;
    }
    if let Some(log_level) = cli.log_level {
        let valid_levels = ["error", "warn", "info", "debug", "trace"];
        if !valid_levels.contains(&log_level.as_str()) {
            anyhow::bail!(
                "Invalid log level: {}. Must be one of: {:?}",
                log_level,
                valid_levels
            );
        }
        config.crawler.log_level = log_level;
    }

    // Parse match mode
    let match_mode = MatchMode::from_str(&cli.match_mode)
        .context("Invalid match mode")?;

    // Create filters from CLI keywords
    let filters = SourceFilters::new(cli.keywords.clone(), match_mode);

    eprintln!(
        "Keywords: {:?} (mode: {:?})",
        cli.keywords,
        match_mode
    );

    // Create shared HTTP client with user agent
    let client = Arc::new(
        reqwest::Client::builder()
            .user_agent(&config.crawler.user_agent)
            .timeout(std::time::Duration::from_secs(30))
            .build()
            .context("Failed to build HTTP client")?
    );

    // Build source instances from config
    let sources: Vec<Box<dyn Source>> = config
        .sources
        .into_iter()
        .filter(|entry| entry.enabled)
        .map(|entry| {
            eprintln!(
                "Enabling source: {} ({})",
                match &entry.config {
                    config::SourceConfig::Reddit(r) => &r.subreddit,
                },
                match &entry.config {
                    config::SourceConfig::Reddit(r) => &r.sort_by,
                }
            );
            build_source(entry.config, client.clone())
        })
        .collect::<Result<Vec<_>>>()
        .context("Failed to build sources")?;

    if sources.is_empty() {
        anyhow::bail!("No enabled sources found in configuration");
    }

    eprintln!("Fetching from {} source(s)...", sources.len());

    // Fetch from all sources concurrently with max_concurrency limit
    let max_concurrency = config.crawler.max_concurrency;
    let all_results = stream::iter(sources)
        .map(|source| {
            let filters = filters.clone();
            async move {
                eprintln!("Fetching from {}...", source.source_id());
                source.fetch(&filters).await
            }
        })
        .buffered(max_concurrency)
        .try_collect::<Vec<Vec<source::Content>>>()
        .await
        .context("Failed to fetch from sources")?;

    // Flatten results
    let all_contents: Vec<source::Content> = all_results
        .into_iter()
        .flatten()
        .collect();

    eprintln!("Fetched {} total posts", all_contents.len());

    // Output results
    let destination = &config.crawler.output_destination;
    output::write_json(&all_contents, destination)
        .context("Failed to write output")?;

    if destination != "stdout" {
        eprintln!("Output written to: {}", destination);
    }

    Ok(())
}
