use crate::config::{SemanticScholarConfig, SemanticScholarMode};
use crate::source::{Content, Source, SourceFilters};
use anyhow::{bail, Context, Result};
use async_trait::async_trait;
use reqwest::StatusCode;
use serde::Deserialize;
use std::sync::Arc;
use std::time::Duration;

// API Response Structures

#[derive(Debug, Deserialize)]
struct SearchResponse {
    #[serde(default)]
    total: Option<i64>,
    #[serde(default)]
    offset: Option<i64>,
    #[serde(default)]
    next: Option<i64>,
    #[serde(default)]
    data: Vec<Paper>,
}

#[derive(Debug, Deserialize)]
struct RecommendationsResponse {
    #[serde(default, rename = "recommendedPapers")]
    recommended_papers: Vec<Paper>,
}

#[derive(Debug, Deserialize, Clone)]
struct Paper {
    #[serde(rename = "paperId")]
    paper_id: String,

    #[serde(default)]
    title: Option<String>,

    #[serde(default, rename = "abstract")]
    abstract_text: Option<String>,

    #[serde(default)]
    year: Option<i32>,

    #[serde(default, rename = "citationCount")]
    citation_count: Option<i32>,

    #[serde(default)]
    url: Option<String>,

    #[serde(default)]
    authors: Vec<Author>,
}

#[derive(Debug, Deserialize, Clone)]
struct Author {
    #[serde(default, rename = "authorId")]
    author_id: Option<String>,

    #[serde(default)]
    name: Option<String>,
}

pub struct SemanticScholarClient {
    client: Arc<reqwest::Client>,
    config: SemanticScholarConfig,
}

impl SemanticScholarClient {
    pub fn new(config: SemanticScholarConfig, client: Arc<reqwest::Client>) -> Result<Self> {
        Ok(Self { client, config })
    }

    /// Fetch with retry logic and exponential backoff
    async fn fetch_with_retry(&self, url: &str) -> Result<reqwest::Response> {
        const MAX_RETRIES: u32 = 3;
        let mut attempt = 0;

        loop {
            let mut request = self.client.get(url);

            // Add API key header if provided
            if let Some(ref api_key) = self.config.api_key {
                request = request.header("x-api-key", api_key);
            }

            let response = request
                .send()
                .await
                .context("Failed to send HTTP request")?;

            match response.status() {
                StatusCode::OK => return Ok(response),
                StatusCode::TOO_MANY_REQUESTS => {
                    if attempt >= MAX_RETRIES {
                        bail!("Rate limited by Semantic Scholar API after {} retries", MAX_RETRIES);
                    }

                    // Try to get Retry-After header, otherwise use exponential backoff
                    let delay_ms = response
                        .headers()
                        .get("Retry-After")
                        .and_then(|v| v.to_str().ok())
                        .and_then(|s| s.parse::<u64>().ok())
                        .map(|s| s * 1000) // Convert seconds to milliseconds
                        .unwrap_or_else(|| 2_u64.pow(attempt) * 1000);

                    eprintln!(
                        "Rate limited by Semantic Scholar API, waiting {}ms (attempt {}/{})",
                        delay_ms,
                        attempt + 1,
                        MAX_RETRIES
                    );
                    tokio::time::sleep(Duration::from_millis(delay_ms)).await;
                    attempt += 1;
                }
                status if status.is_server_error() => {
                    // Retry on 5xx errors (transient server issues)
                    if attempt >= MAX_RETRIES {
                        let error_text = response
                            .text()
                            .await
                            .unwrap_or_else(|_| "Unable to read error message".to_string());
                        bail!("Server error {} after {} retries: {}", status, MAX_RETRIES, error_text);
                    }

                    let delay_ms = 2_u64.pow(attempt) * 1000;
                    eprintln!(
                        "Server error {}, retrying in {}ms (attempt {}/{})",
                        status,
                        delay_ms,
                        attempt + 1,
                        MAX_RETRIES
                    );
                    tokio::time::sleep(Duration::from_millis(delay_ms)).await;
                    attempt += 1;
                }
                status if status.is_client_error() => {
                    // Don't retry 4xx errors (except 429 handled above)
                    let error_text = response
                        .text()
                        .await
                        .unwrap_or_else(|_| "Unable to read error message".to_string());
                    bail!("Client error {}: {}", status, error_text);
                }
                status => {
                    bail!("Unexpected HTTP status: {}", status);
                }
            }
        }
    }

    /// Fetch papers using search endpoint with pagination
    async fn fetch_search_papers(&self, query: &str, year: Option<&str>) -> Result<Vec<Content>> {
        const API_PAGE_SIZE: usize = 100;
        const MAX_OFFSET: usize = 10_000;
        const BASE_URL: &str = "https://api.semanticscholar.org/graph/v1/paper/search";

        let mut all_papers = Vec::new();
        let mut offset = 0;

        while all_papers.len() < self.config.max_results && offset < MAX_OFFSET {
            let page_limit = API_PAGE_SIZE.min(self.config.max_results - all_papers.len());

            // Build URL with query parameters
            let mut url = format!(
                "{}?query={}&offset={}&limit={}&fields=paperId,title,abstract,authors,year,citationCount,url",
                BASE_URL,
                urlencoding::encode(query),
                offset,
                page_limit
            );

            // Add year filter as API parameter (not post-filter)
            if let Some(y) = year {
                url.push_str(&format!("&year={}", urlencoding::encode(y)));
            }

            eprintln!(
                "Fetching Semantic Scholar search results (offset: {}, limit: {})",
                offset, page_limit
            );

            let response = self.fetch_with_retry(&url).await?;
            let search_result: SearchResponse = response
                .json()
                .await
                .context("Failed to parse search response JSON")?;

            if search_result.data.is_empty() {
                break;
            }

            eprintln!("Retrieved {} papers from search", search_result.data.len());
            all_papers.extend(search_result.data);

            // Use 'next' field from API response, fallback to manual increment
            if let Some(next_offset) = search_result.next {
                offset = next_offset as usize;
            } else {
                // No more results available
                break;
            }

            // Rate limiting delay between requests
            if all_papers.len() < self.config.max_results {
                tokio::time::sleep(Duration::from_millis(self.config.rate_limit_delay_ms)).await;
            }
        }

        all_papers.truncate(self.config.max_results);
        eprintln!(
            "Total papers retrieved from search: {} (requested: {})",
            all_papers.len(),
            self.config.max_results
        );

        Ok(self.convert_and_filter(all_papers))
    }

    /// Fetch paper recommendations
    async fn fetch_recommendations(&self, paper_id: &str) -> Result<Vec<Content>> {
        let url = format!(
            "https://api.semanticscholar.org/recommendations/v1/papers/forpaper/{}?fields=paperId,title,abstract,authors,year,citationCount,url",
            urlencoding::encode(paper_id)
        );

        eprintln!("Fetching recommendations for paper: {}", paper_id);

        let response = self.fetch_with_retry(&url).await?;
        let result: RecommendationsResponse = response
            .json()
            .await
            .context("Failed to parse recommendations response JSON")?;

        let mut papers = result.recommended_papers;
        eprintln!("Retrieved {} recommended papers", papers.len());

        papers.truncate(self.config.max_results);
        Ok(self.convert_and_filter(papers))
    }

    /// Convert Paper structs to Content and apply config-level filters
    fn convert_and_filter(&self, papers: Vec<Paper>) -> Vec<Content> {
        papers
            .into_iter()
            .filter_map(|paper| {
                // Skip papers missing both title and abstract
                if paper.title.is_none() && paper.abstract_text.is_none() {
                    return None;
                }

                // Apply min_citations filter
                let citation_count = paper.citation_count.unwrap_or(0);
                if citation_count < self.config.min_citations {
                    return None;
                }

                // Convert to Content
                Some(self.paper_to_content(paper))
            })
            .collect()
    }

    /// Map Paper to Content
    fn paper_to_content(&self, paper: Paper) -> Content {
        let title = paper.title.unwrap_or_else(|| "Untitled".to_string());
        let body = paper.abstract_text.unwrap_or_default();
        let author = paper
            .authors
            .first()
            .and_then(|a| a.name.clone())
            .unwrap_or_else(|| "Unknown".to_string());
        let score = paper.citation_count.unwrap_or(0);

        // Convert year to Unix timestamp (Jan 1, 00:00:00 UTC of that year)
        // Year 1970 = 0, each year ~= 31536000 seconds
        let created_utc = paper.year
            .map(|y| {
                // Days since epoch for Jan 1 of year y
                // Simple calculation: (year - 1970) * 365.25 * 86400
                let years_since_1970 = (y - 1970) as i64;
                years_since_1970 * 31536000 // Approximate (365 days * 24 * 3600)
            })
            .unwrap_or(0);

        Content {
            id: paper.paper_id,
            title,
            body,
            url: paper.url,
            author,
            created_utc,
            score,
            num_comments: 0, // Not applicable for papers
            source_type: self.source_type().to_string(),
            source_id: self.source_id(),
        }
    }
}

#[async_trait]
impl Source for SemanticScholarClient {
    async fn fetch(&self, filters: &SourceFilters) -> Result<Vec<Content>> {
        // Fetch papers based on mode
        let mut contents = match &self.config.mode {
            SemanticScholarMode::Search { query, year } => {
                self.fetch_search_papers(query, year.as_deref()).await?
            }
            SemanticScholarMode::Recommendations { paper_id } => {
                self.fetch_recommendations(paper_id).await?
            }
        };

        // Apply runtime keyword filters
        contents.retain(|c| filters.matches(c));

        eprintln!(
            "After keyword filtering: {} papers (source: {})",
            contents.len(),
            self.source_id()
        );

        Ok(contents)
    }

    fn source_type(&self) -> &str {
        "semantic_scholar"
    }

    fn source_id(&self) -> String {
        match &self.config.mode {
            SemanticScholarMode::Search { query, .. } => {
                // URL-encode query to avoid breaking source_id parsing
                format!("semantic_scholar:search:{}", urlencoding::encode(query))
            }
            SemanticScholarMode::Recommendations { paper_id } => {
                // URL-encode paper_id (may contain special chars like DOIs)
                format!("semantic_scholar:recs:{}", urlencoding::encode(paper_id))
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_paper_to_content_mapping() {
        let config = SemanticScholarConfig {
            mode: SemanticScholarMode::Search {
                query: "test".to_string(),
                year: None,
            },
            max_results: 100,
            min_citations: 0,
            api_key: None,
            rate_limit_delay_ms: 1000,
        };

        let client = Arc::new(reqwest::Client::new());
        let s2_client = SemanticScholarClient::new(config, client).unwrap();

        let paper = Paper {
            paper_id: "abc123".to_string(),
            title: Some("Test Paper".to_string()),
            abstract_text: Some("This is a test abstract".to_string()),
            year: Some(2023),
            citation_count: Some(42),
            url: Some("https://example.com/paper".to_string()),
            authors: vec![Author {
                author_id: Some("author1".to_string()),
                name: Some("Jane Doe".to_string()),
            }],
        };

        let content = s2_client.paper_to_content(paper);

        assert_eq!(content.id, "abc123");
        assert_eq!(content.title, "Test Paper");
        assert_eq!(content.body, "This is a test abstract");
        assert_eq!(content.url, Some("https://example.com/paper".to_string()));
        assert_eq!(content.author, "Jane Doe");
        assert_eq!(content.score, 42);
        // Check timestamp is reasonable (2023 - 1970 = 53 years * 31536000 ~= 1.67B seconds)
        assert!(content.created_utc > 1600000000 && content.created_utc < 1700000000);
        assert_eq!(content.source_type, "semantic_scholar");
        assert_eq!(content.source_id, "semantic_scholar:search:test"); // "test" doesn't need encoding
    }

    #[test]
    fn test_min_citations_filtering() {
        let config = SemanticScholarConfig {
            mode: SemanticScholarMode::Search {
                query: "test".to_string(),
                year: None,
            },
            max_results: 100,
            min_citations: 10,
            api_key: None,
            rate_limit_delay_ms: 1000,
        };

        let client = Arc::new(reqwest::Client::new());
        let s2_client = SemanticScholarClient::new(config, client).unwrap();

        let papers = vec![
            Paper {
                paper_id: "1".to_string(),
                title: Some("High Citation".to_string()),
                abstract_text: Some("Abstract".to_string()),
                year: Some(2020),
                citation_count: Some(50),
                url: None,
                authors: vec![],
            },
            Paper {
                paper_id: "2".to_string(),
                title: Some("Low Citation".to_string()),
                abstract_text: Some("Abstract".to_string()),
                year: Some(2020),
                citation_count: Some(5),
                url: None,
                authors: vec![],
            },
        ];

        let filtered = s2_client.convert_and_filter(papers);
        assert_eq!(filtered.len(), 1);
        assert_eq!(filtered[0].id, "1");
    }

    #[test]
    fn test_parse_search_response() {
        let json = include_str!("../../tests/fixtures/semantic_scholar_search.json");
        let response: SearchResponse = serde_json::from_str(json).unwrap();

        assert_eq!(response.total, Some(1234));
        assert_eq!(response.offset, Some(0));
        assert_eq!(response.next, Some(10));
        assert_eq!(response.data.len(), 3);

        let first_paper = &response.data[0];
        assert_eq!(first_paper.paper_id, "5c5751d45e298cea054f32b392c12c61027d2fe7");
        assert_eq!(
            first_paper.title,
            Some("Construction of the Literature Graph in Semantic Scholar".to_string())
        );
        assert_eq!(first_paper.citation_count, Some(453));
        assert_eq!(first_paper.year, Some(2020));
    }

    #[test]
    fn test_parse_recommendations_response() {
        let json = include_str!("../../tests/fixtures/semantic_scholar_recs.json");
        let response: RecommendationsResponse = serde_json::from_str(json).unwrap();

        assert_eq!(response.recommended_papers.len(), 3);

        let first_rec = &response.recommended_papers[0];
        assert_eq!(first_rec.paper_id, "rec1234567890");
        assert_eq!(
            first_rec.title,
            Some("BERT: Pre-training of Deep Bidirectional Transformers".to_string())
        );
        assert_eq!(first_rec.citation_count, Some(87654));
    }

    #[test]
    fn test_skip_papers_without_title_and_abstract() {
        let config = SemanticScholarConfig {
            mode: SemanticScholarMode::Search {
                query: "test".to_string(),
                year: None,
            },
            max_results: 100,
            min_citations: 0,
            api_key: None,
            rate_limit_delay_ms: 1000,
        };

        let client = Arc::new(reqwest::Client::new());
        let s2_client = SemanticScholarClient::new(config, client).unwrap();

        let papers = vec![
            Paper {
                paper_id: "1".to_string(),
                title: Some("Valid Paper".to_string()),
                abstract_text: None,
                year: Some(2020),
                citation_count: Some(10),
                url: None,
                authors: vec![],
            },
            Paper {
                paper_id: "2".to_string(),
                title: None,
                abstract_text: None, // This should be skipped
                year: Some(2020),
                citation_count: Some(20),
                url: None,
                authors: vec![],
            },
        ];

        let filtered = s2_client.convert_and_filter(papers);
        assert_eq!(filtered.len(), 1);
        assert_eq!(filtered[0].id, "1");
    }
}
