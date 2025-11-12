use crawler::{filter, source::Content};
use std::fs;

#[test]
fn test_reddit_fixture_deserialization() {
    // Read the fixture file
    let fixture_json = fs::read_to_string("tests/fixtures/reddit_hot.json")
        .expect("Failed to read fixture file");

    // Parse as Reddit API response (simulating what we'd get from the API)
    let response: serde_json::Value = serde_json::from_str(&fixture_json)
        .expect("Failed to parse fixture JSON");

    // Verify structure matches Reddit API format
    assert_eq!(response["kind"], "Listing");
    assert!(response["data"]["children"].is_array());

    let children = response["data"]["children"].as_array().unwrap();
    assert_eq!(children.len(), 3, "Fixture should have 3 posts");

    // Verify first post content
    let first_post = &children[0]["data"];
    assert_eq!(first_post["id"], "abc123");
    assert_eq!(first_post["title"], "Rust async programming tips");
    assert_eq!(first_post["author"], "rustacean");
}

#[test]
fn test_filter_with_fixture_data() {
    // Create test contents based on fixture
    let contents = vec![
        Content {
            id: "abc123".to_string(),
            title: "Rust async programming tips".to_string(),
            body: "Here are some tips for working with async Rust and tokio".to_string(),
            url: Some("https://example.com/rust-async".to_string()),
            author: "rustacean".to_string(),
            created_utc: 1234567890,
            score: 150,
            num_comments: 25,
            source: "reddit:rust".to_string(),
        },
        Content {
            id: "def456".to_string(),
            title: "Learning Python for beginners".to_string(),
            body: "This is a Python tutorial".to_string(),
            url: Some("https://example.com/python-tutorial".to_string()),
            author: "pythonista".to_string(),
            created_utc: 1234567891,
            score: 75,
            num_comments: 10,
            source: "reddit:rust".to_string(),
        },
        Content {
            id: "ghi789".to_string(),
            title: "Advanced Rust patterns and async await".to_string(),
            body: "Deep dive into Rust async runtime internals".to_string(),
            url: Some("https://example.com/advanced-rust".to_string()),
            author: "rustexpert".to_string(),
            created_utc: 1234567892,
            score: 200,
            num_comments: 40,
            source: "reddit:rust".to_string(),
        },
    ];

    // Filter for "rust" keyword
    let rust_keywords = vec!["rust".to_string()];
    let filtered = filter::filter_by_keywords(
        contents.clone(),
        &rust_keywords,
        filter::MatchMode::Any,
    );

    // Should match posts with "Rust" in title
    assert_eq!(filtered.len(), 2, "Should match 2 posts with 'rust'");
    assert!(filtered.iter().any(|c| c.id == "abc123"));
    assert!(filtered.iter().any(|c| c.id == "ghi789"));

    // Filter for "async" keyword
    let async_keywords = vec!["async".to_string()];
    let async_filtered = filter::filter_by_keywords(
        contents.clone(),
        &async_keywords,
        filter::MatchMode::Any,
    );

    assert_eq!(async_filtered.len(), 2, "Should match 2 posts with 'async'");

    // Filter for both "rust" AND "async" (ALL mode)
    let both_keywords = vec!["rust".to_string(), "async".to_string()];
    let both_filtered = filter::filter_by_keywords(
        contents,
        &both_keywords,
        filter::MatchMode::All,
    );

    assert_eq!(
        both_filtered.len(),
        2,
        "Should match 2 posts with both 'rust' and 'async'"
    );
}
