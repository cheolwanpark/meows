mod config;
mod filter;
mod output;
mod source;

use anyhow::{Context, Result};
use clap::Parser;
use source::Source;

#[derive(Parser)]
#[command(name = "crawler")]
#[command(version = "0.1.0")]
#[command(about = "Minimal content crawler with keyword filtering", long_about = None)]
struct Cli {
    /// Path to configuration file
    #[arg(short, long, default_value = "config.toml")]
    config: String,

    /// Override output destination (stdout or file path)
    #[arg(short, long)]
    output: Option<String>,
}

#[tokio::main]
async fn main() -> Result<()> {
    // Parse CLI arguments
    let cli = Cli::parse();

    // Load and validate configuration
    let mut config = config::Config::from_file(&cli.config)
        .context("Failed to load configuration")?;

    // Override output destination if specified via CLI
    if let Some(output) = cli.output {
        config.output.destination = output;
    }

    // Create Reddit client with config
    let reddit_config = config.sources.reddit;
    eprintln!(
        "Fetching {} posts from /r/{}...",
        reddit_config.limit, reddit_config.subreddit
    );

    let reddit_client = source::reddit::RedditClient::new(reddit_config)
        .context("Failed to create Reddit client")?;

    let contents = reddit_client
        .fetch()
        .await
        .context("Failed to fetch posts from Reddit")?;

    eprintln!("Fetched {} posts", contents.len());

    // Filter by keywords
    let match_mode = filter::MatchMode::from_str(&config.filter.match_mode);

    eprintln!(
        "Filtering with keywords: {:?} (mode: {:?})",
        config.filter.keywords, match_mode
    );

    let filtered = filter::filter_by_keywords(contents, &config.filter.keywords, match_mode);

    eprintln!("Filtered to {} posts", filtered.len());

    // Output results
    let destination = &config.output.destination;
    output::write_json(&filtered, destination)
        .context("Failed to write output")?;

    if destination != "stdout" {
        eprintln!("Output written to: {}", destination);
    }

    Ok(())
}
