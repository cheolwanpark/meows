package source

import (
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/cheolwanpark/meows/collector/internal/db"
)

func TestParseHTMLComments(t *testing.T) {
	// Test with real HN HTML structure (using img width for depth)
	html := `
	<!DOCTYPE html>
	<html>
	<body>
		<table>
			<tbody>
				<tr class="athing comtr" id="12345">
					<td class="ind"><img src="s.gif" height="1" width="0"></td>
					<td class="default">
						<div>
							<span class="comhead">
								<a href="user?id=testuser" class="hnuser">testuser</a>
								<span class="age" title="2025-11-22T21:50:13"><a href="item?id=12345">4 hours ago</a></span>
							</span>
						</div>
						<div class="commtext c00">This is a test comment</div>
					</td>
				</tr>
				<tr class="athing comtr" id="12346">
					<td class="ind"><img src="s.gif" height="1" width="40"></td>
					<td class="default">
						<div>
							<span class="comhead">
								<a href="user?id=user2" class="hnuser">user2</a>
								<span class="age" title="2025-11-22T22:00:00"><a href="item?id=12346">3 hours ago</a></span>
							</span>
						</div>
						<div class="commtext c00">Reply to first comment</div>
					</td>
				</tr>
			</tbody>
		</table>
	</body>
	</html>
	`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}

	// Create test source
	config := &db.HackerNewsConfig{
		MaxCommentDepth:       3,
		MaxCommentsPerArticle: 100,
	}
	h := &HackerNewsSource{
		config: config,
	}

	comments, err := h.parseHTMLComments(doc)
	if err != nil {
		t.Fatalf("parseHTMLComments failed: %v", err)
	}

	// Verify results
	if len(comments) != 2 {
		t.Errorf("Expected 2 comments, got %d", len(comments))
		return
	}

	// Verify first comment
	if comments[0].externalID != "12345" {
		t.Errorf("Expected externalID '12345', got '%s'", comments[0].externalID)
	}
	if comments[0].author != "testuser" {
		t.Errorf("Expected author 'testuser', got '%s'", comments[0].author)
	}
	if comments[0].depth != 0 {
		t.Errorf("Expected depth 0, got %d", comments[0].depth)
	}
	if comments[0].text != "This is a test comment" {
		t.Errorf("Expected text 'This is a test comment', got '%s'", comments[0].text)
	}

	// Verify timestamp parsing
	expectedTime, _ := time.Parse("2006-01-02T15:04:05", "2025-11-22T21:50:13")
	if !comments[0].timestamp.Equal(expectedTime) {
		t.Errorf("Expected timestamp %v, got %v", expectedTime, comments[0].timestamp)
	}

	// Verify second comment (reply)
	if comments[1].author != "user2" {
		t.Errorf("Expected author 'user2', got '%s'", comments[1].author)
	}
	if comments[1].depth != 1 {
		t.Errorf("Expected depth 1, got %d", comments[1].depth)
	}
}

func TestParseHTMLComments_SkipsDeletedComments(t *testing.T) {
	html := `
	<!DOCTYPE html>
	<html>
	<body>
		<table>
			<tbody>
				<tr class="athing comtr" id="12345">
					<td class="ind"><img src="s.gif" height="1" width="0"></td>
					<td>
						<div>
							<span class="comhead">
								<a href="user?id=testuser" class="hnuser">testuser</a>
								<span class="age" title="2025-11-22T21:50:13"><a href="item?id=12345">4 hours ago</a></span>
							</span>
						</div>
						<div class="commtext c00">Valid comment</div>
					</td>
				</tr>
				<tr class="athing comtr" id="12346">
					<td class="ind"><img src="s.gif" height="1" width="0"></td>
					<td>
						<div>
							<span class="comhead">
								<a href="user?id=user2" class="hnuser">user2</a>
								<span class="age" title="2025-11-22T22:00:00"><a href="item?id=12346">3 hours ago</a></span>
							</span>
						</div>
						<div class="commtext c00">[dead]</div>
					</td>
				</tr>
				<tr class="athing comtr" id="12347">
					<td class="ind"><img src="s.gif" height="1" width="0"></td>
					<td>
						<div>
							<span class="comhead">
								<a href="user?id=user3" class="hnuser">user3</a>
								<span class="age" title="2025-11-22T22:05:00"><a href="item?id=12347">2 hours ago</a></span>
							</span>
						</div>
						<div class="commtext c00">[flagged]</div>
					</td>
				</tr>
			</tbody>
		</table>
	</body>
	</html>
	`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("Failed to parse HTML: %v", err)
	}

	h := &HackerNewsSource{
		config: &db.HackerNewsConfig{
			MaxCommentDepth:       3,
			MaxCommentsPerArticle: 100,
		},
	}

	comments, err := h.parseHTMLComments(doc)
	if err != nil {
		t.Fatalf("parseHTMLComments failed: %v", err)
	}

	// Should only get the valid comment, skipping [dead] and [flagged]
	if len(comments) != 1 {
		t.Errorf("Expected 1 comment (skipped dead/flagged), got %d", len(comments))
	}

	if len(comments) > 0 && comments[0].externalID != "12345" {
		t.Errorf("Expected only valid comment with ID 12345, got %s", comments[0].externalID)
	}
}

func TestBuildCommentsWithParents(t *testing.T) {
	// Create test HTML comments with depth structure
	htmlComments := []htmlComment{
		{
			externalID: "1",
			author:     "user1",
			text:       "Root comment",
			depth:      0,
			timestamp:  time.Now(),
		},
		{
			externalID: "2",
			author:     "user2",
			text:       "Reply to root",
			depth:      1,
			timestamp:  time.Now(),
		},
		{
			externalID: "3",
			author:     "user3",
			text:       "Reply to reply",
			depth:      2,
			timestamp:  time.Now(),
		},
		{
			externalID: "4",
			author:     "user4",
			text:       "Another reply to root",
			depth:      1,
			timestamp:  time.Now(),
		},
	}

	h := &HackerNewsSource{
		config: &db.HackerNewsConfig{
			MaxCommentDepth:       10,
			MaxCommentsPerArticle: 100,
		},
	}

	comments, err := h.buildCommentsWithParents(htmlComments, "test-article-id")
	if err != nil {
		t.Fatalf("buildCommentsWithParents failed: %v", err)
	}

	if len(comments) != 4 {
		t.Fatalf("Expected 4 comments, got %d", len(comments))
	}

	// Verify root comment (depth 0) has no parent
	if comments[0].ParentID != nil {
		t.Errorf("Root comment should have nil parent, got %v", comments[0].ParentID)
	}
	if comments[0].Depth != 0 {
		t.Errorf("Expected depth 0, got %d", comments[0].Depth)
	}

	// Verify first reply (depth 1) has root as parent
	if comments[1].ParentID == nil {
		t.Error("Reply should have parent")
	} else if *comments[1].ParentID != comments[0].ID {
		t.Errorf("Reply parent mismatch: expected %s, got %s", comments[0].ID, *comments[1].ParentID)
	}
	if comments[1].Depth != 1 {
		t.Errorf("Expected depth 1, got %d", comments[1].Depth)
	}

	// Verify nested reply (depth 2) has correct parent
	if comments[2].ParentID == nil {
		t.Error("Nested reply should have parent")
	} else if *comments[2].ParentID != comments[1].ID {
		t.Errorf("Nested reply parent mismatch: expected %s, got %s", comments[1].ID, *comments[2].ParentID)
	}
	if comments[2].Depth != 2 {
		t.Errorf("Expected depth 2, got %d", comments[2].Depth)
	}

	// Verify second reply to root (depth 1) has root as parent
	if comments[3].ParentID == nil {
		t.Error("Second reply should have parent")
	} else if *comments[3].ParentID != comments[0].ID {
		t.Errorf("Second reply parent mismatch: expected %s, got %s", comments[0].ID, *comments[3].ParentID)
	}
}

func TestBuildCommentsWithParents_OrphanedComment(t *testing.T) {
	// Create test with orphaned comment (depth 2 without depth 1 parent)
	htmlComments := []htmlComment{
		{
			externalID: "1",
			author:     "user1",
			text:       "Root comment",
			depth:      0,
			timestamp:  time.Now(),
		},
		{
			externalID: "2",
			author:     "user2",
			text:       "Orphaned comment (no depth-1 parent)",
			depth:      2, // Skipped depth 1!
			timestamp:  time.Now(),
		},
	}

	h := &HackerNewsSource{
		config: &db.HackerNewsConfig{
			MaxCommentDepth:       10,
			MaxCommentsPerArticle: 100,
		},
	}

	_, err := h.buildCommentsWithParents(htmlComments, "test-article-id")
	if err == nil {
		t.Error("Expected error for orphaned comment, got nil")
	}

	if !strings.Contains(err.Error(), "orphaned comment") {
		t.Errorf("Expected 'orphaned comment' error, got: %v", err)
	}
}

func TestBuildCommentsWithParents_DepthLimit(t *testing.T) {
	// Create comments with various depths
	htmlComments := []htmlComment{
		{externalID: "1", author: "u1", text: "Depth 0", depth: 0, timestamp: time.Now()},
		{externalID: "2", author: "u2", text: "Depth 1", depth: 1, timestamp: time.Now()},
		{externalID: "3", author: "u3", text: "Depth 2", depth: 2, timestamp: time.Now()},
		{externalID: "4", author: "u4", text: "Depth 3", depth: 3, timestamp: time.Now()},
		{externalID: "5", author: "u5", text: "Depth 4", depth: 4, timestamp: time.Now()},
	}

	h := &HackerNewsSource{
		config: &db.HackerNewsConfig{
			MaxCommentDepth:       2, // Limit to depth 2
			MaxCommentsPerArticle: 100,
		},
	}

	comments, err := h.buildCommentsWithParents(htmlComments, "test-article-id")
	if err != nil {
		t.Fatalf("buildCommentsWithParents failed: %v", err)
	}

	// Should only get comments up to depth 2 (0, 1, 2)
	if len(comments) != 3 {
		t.Errorf("Expected 3 comments (depth 0-2), got %d", len(comments))
	}

	// Verify max depth is 2
	for _, c := range comments {
		if c.Depth > 2 {
			t.Errorf("Found comment with depth %d, expected max 2", c.Depth)
		}
	}
}

func TestValidateHTMLStructure(t *testing.T) {
	tests := []struct {
		name     string
		html     string
		storyID  int
		expected bool
	}{
		{
			name: "valid structure",
			html: `
				<!DOCTYPE html>
				<html><body>
					<table><tbody>
						<tr class="athing" id="12345"><td class="title"><span class="titleline"><a href="#">Story title</a></span></td></tr>
						<tr><td><form method="post" action="comment"></form></td></tr>
						<tr class="athing comtr" id="100">
							<td class="ind"></td>
							<td><div class="commtext">Comment</div></td>
						</tr>
					</tbody></table>
				</body></html>
			`,
			storyID:  12345,
			expected: true,
		},
		{
			name: "no comments (valid - empty thread)",
			html: `
				<!DOCTYPE html>
				<html><body>
					<table><tbody>
						<tr class="athing" id="12345"><td class="title"><span class="titleline"><a href="#">Story title</a></span></td></tr>
						<tr><td><form method="post" action="comment"></form></td></tr>
					</tbody></table>
				</body></html>
			`,
			storyID:  12345,
			expected: true,
		},
		{
			name: "pagination detected",
			html: `
				<!DOCTYPE html>
				<html><body>
					<table><tbody>
						<tr class="athing" id="12345"><td class="title"><span class="titleline"><a href="#">Story title</a></span></td></tr>
						<tr><td><form method="post" action="comment"></form></td></tr>
						<tr class="athing comtr" id="100">
							<td class="ind"></td>
							<td><div class="commtext">Comment</div></td>
						</tr>
						<tr><td><a class="morelink" href="item?id=12345&p=2">More</a></td></tr>
					</tbody></table>
				</body></html>
			`,
			storyID:  12345,
			expected: false, // Should fail due to pagination
		},
		{
			name: "missing story",
			html: `
				<!DOCTYPE html>
				<html><body>
					<table><tbody>
						<tr><td><form method="post" action="comment"></form></td></tr>
						<tr class="athing comtr" id="100">
							<td class="ind"></td>
							<td><div class="commtext">Comment</div></td>
						</tr>
					</tbody></table>
				</body></html>
			`,
			storyID:  12345,
			expected: false,
		},
		{
			name: "missing comment form",
			html: `
				<!DOCTYPE html>
				<html><body>
					<table><tbody>
						<tr class="athing" id="12345"><td class="title"><span class="titleline"><a href="#">Story title</a></span></td></tr>
						<tr class="athing comtr" id="100">
							<td class="ind"></td>
							<td><div class="commtext">Comment</div></td>
						</tr>
					</tbody></table>
				</body></html>
			`,
			storyID:  12345,
			expected: false,
		},
		{
			name: "broken comment structure - missing commtext",
			html: `
				<!DOCTYPE html>
				<html><body>
					<table><tbody>
						<tr class="athing" id="12345"><td class="title"><span class="titleline"><a href="#">Story title</a></span></td></tr>
						<tr><td><form method="post" action="comment"></form></td></tr>
						<tr class="athing comtr" id="100">
							<td class="ind"></td>
							<td></td>
						</tr>
					</tbody></table>
				</body></html>
			`,
			storyID:  12345,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(tt.html))
			if err != nil {
				t.Fatalf("Failed to parse HTML: %v", err)
			}

			h := &HackerNewsSource{}
			result := h.validateHTMLStructure(doc, tt.storyID)
			if result != tt.expected {
				t.Errorf("Expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestBuildCommentsWithParents_MaxCommentsPerArticle(t *testing.T) {
	// Create many comments
	var htmlComments []htmlComment
	for i := 0; i < 100; i++ {
		htmlComments = append(htmlComments, htmlComment{
			externalID: string(rune(i)),
			author:     "user",
			text:       "Comment",
			depth:      0,
			timestamp:  time.Now(),
		})
	}

	h := &HackerNewsSource{
		config: &db.HackerNewsConfig{
			MaxCommentDepth:       10,
			MaxCommentsPerArticle: 50, // Limit to 50
		},
	}

	comments, err := h.buildCommentsWithParents(htmlComments, "test-article-id")
	if err != nil {
		t.Fatalf("buildCommentsWithParents failed: %v", err)
	}

	// Should only get 50 comments
	if len(comments) != 50 {
		t.Errorf("Expected 50 comments (limit), got %d", len(comments))
	}
}
