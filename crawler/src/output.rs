use crate::source::Content;
use anyhow::{Context, Result};
use std::io::{self, Write};
use std::path::Path;

/// Write filtered content as JSON to stdout or a file
///
/// # Arguments
/// * `contents` - Vector of content items to output
/// * `destination` - "stdout" or file path
///
/// # Returns
/// Result indicating success or failure
///
/// For file output, uses atomic writes (temp file + rename) to avoid
/// partial writes on crashes.
pub fn write_json(contents: &[Content], destination: &str) -> Result<()> {
    let json =
        serde_json::to_string_pretty(contents).context("Failed to serialize content to JSON")?;

    if destination == "stdout" {
        write_to_stdout(&json)?;
    } else {
        write_to_file(&json, destination)?;
    }

    Ok(())
}

/// Write JSON to stdout
fn write_to_stdout(json: &str) -> Result<()> {
    let stdout = io::stdout();
    let mut handle = stdout.lock();

    writeln!(handle, "{}", json).context("Failed to write to stdout")?;

    Ok(())
}

/// Write JSON to a file using atomic write pattern
fn write_to_file(json: &str, path: &str) -> Result<()> {
    let file_path = Path::new(path);

    // Get parent directory for tempfile, or use current dir
    let parent_dir = file_path
        .parent()
        .filter(|p| !p.as_os_str().is_empty())
        .unwrap_or_else(|| Path::new("."));

    // Create temporary file in same directory as target
    let mut temp_file =
        tempfile::NamedTempFile::new_in(parent_dir).context("Failed to create temporary file")?;

    // Write content and sync to disk
    temp_file
        .write_all(json.as_bytes())
        .context("Failed to write JSON to temporary file")?;

    temp_file
        .as_file()
        .sync_all()
        .context("Failed to sync temporary file to disk")?;

    // Atomically persist (rename) temp file to final destination
    // This handles cross-platform atomicity and auto-cleanup on error
    temp_file
        .persist(path)
        .context(format!("Failed to persist file to {}", path))?;

    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::fs;
    use tempfile::TempDir;

    fn create_test_content() -> Content {
        Content {
            id: "test123".to_string(),
            title: "Test Post".to_string(),
            body: "Test body".to_string(),
            url: Some("https://example.com".to_string()),
            author: "testuser".to_string(),
            created_utc: 1234567890,
            score: 100,
            num_comments: 10,
            source_type: "test".to_string(),
            source_id: "test:1".to_string(),
        }
    }

    #[test]
    fn test_write_to_file() {
        let temp_dir = TempDir::new().unwrap();
        let file_path = temp_dir.path().join("output.json");
        let file_path_str = file_path.to_str().unwrap();

        let contents = vec![create_test_content()];
        write_json(&contents, file_path_str).unwrap();

        // Verify file was created
        assert!(file_path.exists());

        // Verify JSON content
        let content = fs::read_to_string(&file_path).unwrap();
        let parsed: Vec<Content> = serde_json::from_str(&content).unwrap();
        assert_eq!(parsed.len(), 1);
        assert_eq!(parsed[0].title, "Test Post");
    }

    #[test]
    fn test_json_serialization() {
        let contents = vec![create_test_content()];
        let json = serde_json::to_string_pretty(&contents).unwrap();

        assert!(json.contains("Test Post"));
        assert!(json.contains("testuser"));
        assert!(json.contains("test123"));
    }
}
