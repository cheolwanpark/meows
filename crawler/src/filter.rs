use crate::source::Content;

/// Match mode for keyword filtering
#[derive(Debug, Clone, PartialEq)]
pub enum MatchMode {
    /// Match if ANY keyword is found (OR logic)
    Any,
    /// Match only if ALL keywords are found (AND logic)
    All,
}

impl MatchMode {
    pub fn from_str(s: &str) -> Self {
        match s.to_lowercase().as_str() {
            "all" => MatchMode::All,
            _ => MatchMode::Any,
        }
    }
}

/// Filter content by keywords using simple substring matching
///
/// # Arguments
/// * `contents` - Vector of content items to filter
/// * `keywords` - List of keywords to search for
/// * `mode` - Match mode (Any or All)
///
/// # Returns
/// Filtered vector containing only items that match the keyword criteria
///
/// # Example
/// ```
/// use crawler::filter::{filter_by_keywords, MatchMode};
/// use crawler::source::Content;
///
/// let posts = vec![Content {
///     id: "1".to_string(),
///     title: "Rust async".to_string(),
///     body: "".to_string(),
///     url: None,
///     author: "test".to_string(),
///     created_utc: 0,
///     score: 0,
///     num_comments: 0,
///     source: "test".to_string(),
/// }];
/// let keywords = vec!["rust".to_string(), "async".to_string()];
/// let filtered = filter_by_keywords(posts, &keywords, MatchMode::Any);
/// ```
pub fn filter_by_keywords(
    contents: Vec<Content>,
    keywords: &[String],
    mode: MatchMode,
) -> Vec<Content> {
    // Pre-lowercase keywords once to avoid repeated allocations
    let lowercase_keywords: Vec<String> = keywords.iter().map(|k| k.to_lowercase()).collect();

    contents
        .into_iter()
        .filter(|content| matches_keywords(content, &lowercase_keywords, &mode))
        .collect()
}

/// Check if a content item matches the keyword criteria
fn matches_keywords(content: &Content, lowercase_keywords: &[String], mode: &MatchMode) -> bool {
    // Lowercase title and body once each
    let title_lower = content.title.to_lowercase();
    let body_lower = content.body.to_lowercase();

    match mode {
        MatchMode::Any => {
            // Match if ANY keyword is found in either title or body
            lowercase_keywords
                .iter()
                .any(|keyword| title_lower.contains(keyword) || body_lower.contains(keyword))
        }
        MatchMode::All => {
            // Match only if ALL keywords are found in title+body combined
            lowercase_keywords
                .iter()
                .all(|keyword| title_lower.contains(keyword) || body_lower.contains(keyword))
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn create_test_content(title: &str, body: &str) -> Content {
        Content {
            id: "test123".to_string(),
            title: title.to_string(),
            body: body.to_string(),
            url: None,
            author: "testuser".to_string(),
            created_utc: 1234567890,
            score: 100,
            num_comments: 10,
            source: "test".to_string(),
        }
    }

    // Helper to lowercase keywords for testing matches_keywords directly
    fn lowercase_keywords(keywords: &[String]) -> Vec<String> {
        keywords.iter().map(|k| k.to_lowercase()).collect()
    }

    #[test]
    fn test_match_any_mode() {
        let content = create_test_content("Learning Rust programming", "Async is cool");
        let keywords = vec!["rust".to_string(), "python".to_string()];
        let lowercase = lowercase_keywords(&keywords);

        assert!(matches_keywords(&content, &lowercase, &MatchMode::Any));
    }

    #[test]
    fn test_match_all_mode() {
        let content = create_test_content("Learning Rust programming", "Async is cool");
        let keywords = vec!["rust".to_string(), "async".to_string()];
        let lowercase = lowercase_keywords(&keywords);

        assert!(matches_keywords(&content, &lowercase, &MatchMode::All));
    }

    #[test]
    fn test_match_all_mode_fails() {
        let content = create_test_content("Learning Rust programming", "Functions are cool");
        let keywords = vec!["rust".to_string(), "async".to_string()];
        let lowercase = lowercase_keywords(&keywords);

        assert!(!matches_keywords(&content, &lowercase, &MatchMode::All));
    }

    #[test]
    fn test_case_insensitive() {
        let content = create_test_content("RUST Programming", "async AWAIT");
        let keywords = vec!["rust".to_string(), "Async".to_string()];
        let lowercase = lowercase_keywords(&keywords);

        assert!(matches_keywords(&content, &lowercase, &MatchMode::All));
    }

    #[test]
    fn test_filter_by_keywords() {
        let contents = vec![
            create_test_content("Rust async await", "body1"),
            create_test_content("Python tutorial", "body2"),
            create_test_content("Rust basics", "body3"),
        ];

        let keywords = vec!["rust".to_string()];
        let filtered = filter_by_keywords(contents, &keywords, MatchMode::Any);

        assert_eq!(filtered.len(), 2);
        assert!(filtered.iter().all(|c| c.title.contains("Rust")));
    }
}
