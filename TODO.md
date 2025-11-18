# Profile Feature Implementation - TODO

## Overview
This document tracks the implementation of a Netflix-style multi-profile system for the Meows news aggregator. Each profile has isolated sources/articles, can like articles, and receives AI-generated character descriptions via Google's Gemini API.

**Original Plan**: See implementation plan from 2025-11-18 session
**Tech Stack**: Go + SQLite + Chi router + Templ + HTMX + Alpine.js + Gemini API

---

## 📊 PROGRESS SUMMARY (Updated 2025-11-18)

### Backend: ~75% Complete ✅
- ✅ All database schemas and models
- ✅ Gemini API client with token counting
- ✅ Profile update service with milestone state machine
- ✅ All API endpoints (profiles, likes, articles)
- ✅ Configuration and initialization
- ✅ **CRITICAL FIXES**: LikeArticle race condition (transaction wrapper)
- ✅ **CRITICAL FIXES**: Weekly cron job for profile updates
- ✅ Article endpoints now include like status with profile_id parameter
- ✅ Cookie middleware for profile context

### Frontend: 0% Complete ❌
- ❌ Frontend cookie middleware (not started)
- ❌ Profile setup page (not started)
- ❌ Profile switcher in header (not started)
- ❌ Like button component (not started)
- ❌ Profile management page (not started)

### Next Steps:
1. Frontend implementation (tasks 5-9)
2. End-to-end testing
3. Optional improvements (concurrent UpdateCharacter mutex, better error handling)

---

## ✅ COMPLETED (Backend)

### Database Schema
- ✅ Added `profiles` table with: id, nickname, user_description, character, character_status, character_error, milestone, updated_at, created_at
- ✅ Added `likes` table with: id, profile_id, article_id, created_at + UNIQUE(profile_id, article_id)
- ✅ Added `profile_id` FK to `sources` and `articles` tables
- ✅ Added 6 performance indexes
- **File**: `collector/internal/db/db.go`

### Models
- ✅ Profile and Like structs with JSON tags
- ✅ Updated Source and Article to include ProfileID field
- **File**: `collector/internal/db/models.go`

### Gemini Client
- ✅ Low-level client with retry logic (max 3, exponential backoff, 30s timeout)
- ✅ Local token counting (using `google.golang.org/genai/tokenizer`)
- ✅ Constants: FLASH, FLASH_LITE, MAX_TOKENS (900K)
- **File**: `collector/internal/gemini/gemini.go`
- **Package**: `google.golang.org/genai v1.35.0`

### Profile Update Service
- ✅ Progressive prompt building with token limit checking
- ✅ Milestone state machine (init→3→10→20→weekly)
- ✅ Transaction pattern fixed (no network I/O during transactions)
- ✅ Separated into: setUpdatingStatus → fetchData → callGemini → updateResult
- **File**: `collector/internal/profile/profile_update.go`

### API Endpoints
- ✅ POST /profiles - Create profile, trigger async character generation
- ✅ GET /profiles - List all profiles
- ✅ GET /profiles/{id} - Get profile (for polling character_status)
- ✅ PATCH /profiles/{id} - Update profile, regenerate if description changed
- ✅ DELETE /profiles/{id} - Delete with CASCADE
- ✅ POST /articles/{id}/like - Create like, check milestone
- ✅ DELETE /likes/{id} - Delete like
- **Files**: `collector/internal/api/handlers.go`, `router.go`, `dto.go`

### Configuration
- ✅ Added GEMINI_API_KEY env var
- ✅ Added PROFILE_DAILY_CRON env var (default: "0 1 * * *")
- ✅ GeminiConfig and ProfileConfig structs
- **File**: `collector/internal/config/config.go`

### Main Initialization
- ✅ Gemini client initialization
- ✅ ProfileService initialization
- ✅ Router updated with profileService
- **File**: `collector/cmd/server/main.go`

### Fixes Applied
- ✅ CreateSource now requires and validates profile_id
- ✅ UpdateCharacter transaction pattern fixed (short transactions only)
- ✅ Milestone transition bug fixed (20→weekly now triggers final update)

---

## ✅ CRITICAL ISSUES - COMPLETED (2025-11-18)

### 1. LikeArticle Race Condition ✅ FIXED
**Problem**: Multiple concurrent likes can trigger duplicate milestone updates and multiple character generation jobs.

**Location**: `collector/internal/api/handlers.go:1068-1167` (LikeArticle function)

**Current Code** (BROKEN):
```go
func (h *Handler) LikeArticle(w http.ResponseWriter, r *http.Request) {
    // ... validation ...

    // INSERT like (line 1095)
    _, err = h.db.Exec(`INSERT INTO likes ...`)

    // Check milestone (line 1112) - NOT IN TRANSACTION
    err = h.db.QueryRow(`SELECT COUNT(*) ...`).Scan(&likeCount, &currentMilestone)

    if shouldUpdate {
        // Update milestone - RACE CONDITION HERE
        _, err = h.db.Exec("UPDATE profiles SET milestone = ? WHERE id = ?", ...)
        h.profileService.UpdateCharacter(r.Context(), req.ProfileID)
    }

    w.WriteHeader(http.StatusCreated)
    json.NewEncoder(w).Encode(like)
}
```

**Required Fix**:
```go
func (h *Handler) LikeArticle(w http.ResponseWriter, r *http.Request) {
    articleID := chi.URLParam(r, "id")
    var req CreateLikeRequest

    // ... validation code stays the same ...

    // BEGIN TRANSACTION
    tx, err := h.db.Begin()
    if err != nil {
        respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to begin transaction: %v", err))
        return
    }
    defer tx.Rollback()

    // Create like WITHIN TRANSACTION
    like := &db.Like{
        ID:        uuid.New().String(),
        ProfileID: req.ProfileID,
        ArticleID: articleID,
        CreatedAt: time.Now(),
    }

    _, err = tx.Exec(`
        INSERT INTO likes (id, profile_id, article_id, created_at)
        VALUES (?, ?, ?, ?)
    `, like.ID, like.ProfileID, like.ArticleID, like.CreatedAt)

    if err != nil {
        if isLikeUniqueViolation(err) {
            respondError(w, http.StatusConflict, "article already liked")
            return
        }
        respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to create like: %v", err))
        return
    }

    // Check milestone WITHIN SAME TRANSACTION
    var likeCount int
    var currentMilestone string
    err = tx.QueryRow(`
        SELECT COUNT(*), p.milestone
        FROM likes l
        JOIN profiles p ON l.profile_id = p.id
        WHERE l.profile_id = ?
        GROUP BY p.milestone
    `, req.ProfileID).Scan(&likeCount, &currentMilestone)

    var shouldUpdate bool
    var newMilestone string

    if err == nil {
        shouldUpdate, newMilestone = h.profileService.CheckMilestone(currentMilestone, likeCount)
        if shouldUpdate {
            // Update milestone WITHIN TRANSACTION
            _, err = tx.Exec("UPDATE profiles SET milestone = ? WHERE id = ?", newMilestone, req.ProfileID)
            if err != nil {
                respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to update milestone: %v", err))
                return
            }
        }
    }

    // COMMIT TRANSACTION
    if err := tx.Commit(); err != nil {
        respondError(w, http.StatusInternalServerError, fmt.Sprintf("failed to commit: %v", err))
        return
    }

    // Trigger character update AFTER transaction commits
    if shouldUpdate {
        h.profileService.UpdateCharacter(r.Context(), req.ProfileID)
    }

    w.WriteHeader(http.StatusCreated)
    json.NewEncoder(w).Encode(like)
}
```

**Additional Fix** - Improve milestone SQL query:
```go
// Current query uses unnecessary GROUP BY
// Replace with:
err = tx.QueryRow(`SELECT milestone FROM profiles WHERE id = ?`, req.ProfileID).Scan(&currentMilestone)
err = tx.QueryRow(`SELECT COUNT(*) FROM likes WHERE profile_id = ?`, req.ProfileID).Scan(&likeCount)
```

**Status**: ✅ FIXED - Transaction wrapper added, milestone updates now atomic

---

### 2. Weekly Cron Not Implemented ✅ FIXED
**Problem**: Config loads `PROFILE_DAILY_CRON` but it's never used. Weekly updates for profiles with 20+ likes will never run.

**Where to Add**: `collector/internal/scheduler/scheduler.go` OR `collector/cmd/server/main.go`

**Option A - Add to Existing Scheduler** (RECOMMENDED):

1. Add method to `Scheduler` struct in `collector/internal/scheduler/scheduler.go`:

```go
// Add this method to the Scheduler struct
func (s *Scheduler) CheckProfileUpdates() {
    ctx := context.Background()

    // Query profiles needing weekly updates
    rows, err := s.db.Query(`
        SELECT id
        FROM profiles
        WHERE milestone = 'weekly'
          AND (updated_at IS NULL OR updated_at < datetime('now', '-7 days'))
    `)
    if err != nil {
        slog.Error("Failed to query profiles for weekly update", "error", err)
        return
    }
    defer rows.Close()

    var profileIDs []string
    for rows.Next() {
        var id string
        if err := rows.Scan(&id); err != nil {
            slog.Error("Failed to scan profile ID", "error", err)
            continue
        }
        profileIDs = append(profileIDs, id)
    }

    slog.Info("Weekly profile update check", "profiles_to_update", len(profileIDs))

    // Update each profile (max 5 concurrent as per config)
    for _, profileID := range profileIDs {
        // Call profile service (pass via constructor or add field)
        // profileService.UpdateCharacter(ctx, profileID)
    }
}
```

2. Modify `Scheduler` struct to include `profileService`:

```go
// In scheduler/scheduler.go
type Scheduler struct {
    cron           *cron.Cron
    db             *db.DB
    config         *config.CollectorConfig
    rateLimiters   map[string]*rate.Limiter
    profileService *profile.UpdateService  // ADD THIS
}

// Update New() constructor
func New(cfg *config.CollectorConfig, database *db.DB, profService *profile.UpdateService) (*Scheduler, error) {
    // ... existing code ...

    s := &Scheduler{
        cron:           c,
        db:             database,
        config:         cfg,
        rateLimiters:   rateLimiters,
        profileService: profService,  // ADD THIS
    }

    // Add profile update cron job
    _, err = c.AddFunc(cfg.Profile.DailyCronExpr, func() {
        s.CheckProfileUpdates()
    })
    if err != nil {
        return nil, fmt.Errorf("failed to schedule profile updates: %w", err)
    }

    return s, nil
}
```

3. Update `main.go` to pass `profileService` to scheduler:

```go
// In collector/cmd/server/main.go
// Current line ~83:
sched, err := scheduler.New(&cfg.Collector, database)

// Change to:
sched, err := scheduler.New(&cfg.Collector, database, profileService)
```

**Option B - Separate Cron** (Alternative):

Create separate cron in `main.go`:

```go
// In main.go, after scheduler initialization
profileCron := cron.New()
_, err = profileCron.AddFunc(cfg.Collector.Profile.DailyCronExpr, func() {
    // Query and update profiles
    rows, _ := database.Query(`
        SELECT id FROM profiles
        WHERE milestone = 'weekly'
          AND (updated_at IS NULL OR updated_at < datetime('now', '-7 days'))
    `)
    defer rows.Close()

    for rows.Next() {
        var profileID string
        rows.Scan(&profileID)
        profileService.UpdateCharacter(context.Background(), profileID)
    }
})
profileCron.Start()
```

**Status**: ✅ FIXED - Weekly cron job implemented in scheduler (Option A)
- Modified `scheduler.go` to include `profileService` field
- Added `CheckProfileUpdates()` method to query and update profiles
- Updated `main.go` to pass `profileService` to scheduler
- Cron job registered with `PROFILE_DAILY_CRON` schedule

---

## ⚠️ REMAINING BACKEND TASKS

### 3. Modify Article Endpoints to Include Like Status ✅ COMPLETED
**Files**: `collector/internal/api/handlers.go`, `collector/internal/api/dto.go`

**Current**: `GET /articles` and `GET /articles/{id}` don't return like information

**Needed**:
1. Add optional `profile_id` query parameter to ListArticles
2. Join with likes table when profile_id provided
3. Add `liked` boolean field to Article response

**Example Implementation**:

```go
// In handlers.go, modify ListArticles
func (h *Handler) ListArticles(w http.ResponseWriter, r *http.Request) {
    sourceID := r.URL.Query().Get("source_id")
    profileID := r.URL.Query().Get("profile_id")  // ADD THIS
    // ... existing limit, offset, since parsing ...

    query := `
        SELECT a.id, a.source_id, a.external_id, a.profile_id, a.title,
               a.author, a.content, a.url, a.written_at, a.metadata, a.created_at,
               CASE WHEN l.id IS NOT NULL THEN 1 ELSE 0 END as liked
        FROM articles a
    `

    // Add LEFT JOIN if profile_id provided
    if profileID != "" {
        query += `
        LEFT JOIN likes l ON a.id = l.article_id AND l.profile_id = ?
        `
        args = append(args, profileID)
    }

    query += ` WHERE 1=1`

    // ... rest of existing WHERE clauses ...

    // Scan including liked field
    var liked int
    err = rows.Scan(
        &article.ID, &article.SourceID, &article.ExternalID, &article.ProfileID,
        &article.Title, &article.Author, &article.Content, &article.URL,
        &article.WrittenAt, &metadata, &article.CreatedAt, &liked,
    )

    // Add liked to response (need to update Article model or create ArticleWithLike DTO)
}
```

**Status**: ✅ COMPLETED
- Created `ArticleWithLikeStatus` DTO in `dto.go`
- Modified `ListArticles` to accept optional `profile_id` query parameter
- Added LEFT JOIN with likes table when profile_id provided
- Returns `ArticleWithLikeStatus` with `liked` boolean and `like_id` fields

### 4. Add Cookie Middleware for Profile Context ✅ COMPLETED
**Files**: `collector/internal/api/middleware.go`

**Purpose**: Read `current_profile_id` cookie, validate, inject into request context

**Implementation**:

```go
// Create collector/internal/api/middleware.go if it doesn't exist
package api

import (
    "context"
    "database/sql"
    "net/http"
    "github.com/cheolwanpark/meows/collector/internal/db"
)

type contextKey string

const ProfileIDKey contextKey = "profile_id"

// ProfileContext middleware reads and validates profile_id cookie
func ProfileContext(database *db.DB) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            cookie, err := r.Cookie("current_profile_id")

            if err != nil || cookie.Value == "" {
                // No profile cookie - continue without profile context
                next.ServeHTTP(w, r)
                return
            }

            // Validate profile exists
            var exists bool
            err = database.QueryRow("SELECT EXISTS(SELECT 1 FROM profiles WHERE id = ?)", cookie.Value).Scan(&exists)

            if err != nil || !exists {
                // Invalid profile - clear cookie
                http.SetCookie(w, &http.Cookie{
                    Name:     "current_profile_id",
                    Value:    "",
                    Path:     "/",
                    MaxAge:   -1,
                    HttpOnly: true,
                    SameSite: http.SameSiteLaxMode,
                })
                next.ServeHTTP(w, r)
                return
            }

            // Inject profile ID into context
            ctx := context.WithValue(r.Context(), ProfileIDKey, cookie.Value)
            next.ServeHTTP(w, r.WithContext(ctx))
        })
    }
}

// GetProfileID extracts profile ID from request context
func GetProfileID(r *http.Request) (string, bool) {
    profileID, ok := r.Context().Value(ProfileIDKey).(string)
    return profileID, ok
}
```

**Add to router**:
```go
// In router.go
r.Use(ProfileContext(database))
```

**Status**: ✅ COMPLETED
- Added `ProfileContext` middleware to `middleware.go`
- Middleware reads and validates `current_profile_id` cookie
- Injects profile ID into request context if valid
- Clears invalid cookies automatically
- Added `GetProfileID(r)` helper function for handlers
- Middleware registered in router

**Use in handlers**:
```go
// In handlers that need profile context
profileID, ok := GetProfileID(r)
if !ok {
    respondError(w, http.StatusBadRequest, "profile context required")
    return
}
```

---

## 🎨 FRONTEND TASKS (Not Started)

### 5. Frontend Cookie Middleware
**File**: `front/internal/middleware/profile.go`

**Similar to backend but**:
- Sets cookie on profile switch
- Redirects to setup if no profiles exist
- Cookie flags: HttpOnly, Secure (if HTTPS), SameSite=Lax, Max-Age=30 days

### 6. Profile Setup Page
**File**: `front/templates/pages/profile-setup.templ`

**Features**:
- Form: nickname input + user_description textarea
- Submit via HTMX: `hx-post="/api/profiles"` with loading spinner
- Alpine.js polling after submit (every 2s):
  - Poll `GET /profiles/{id}`
  - Check `character_status`
  - If 'ready': redirect to home
  - If 'error': show error + retry button
  - If 'pending'|'updating': continue polling

**Example Template**:
```templ
package pages

import "github.com/cheolwanpark/meows/front/templates/layouts"

templ ProfileSetup() {
    @layouts.Base("Setup Profile") {
        <div x-data="profileSetup()" class="max-w-2xl mx-auto p-8">
            <h1 class="text-3xl font-bold mb-6">Create Your Profile</h1>

            <form hx-post="/api/profiles"
                  hx-trigger="submit"
                  @htmx:after-request="handleResponse($event)">

                <div class="mb-4">
                    <label class="block mb-2">Nickname</label>
                    <input type="text" name="nickname" required
                           class="w-full p-2 border rounded"/>
                </div>

                <div class="mb-4">
                    <label class="block mb-2">Describe your interests</label>
                    <textarea name="user_description" rows="4" required
                              class="w-full p-2 border rounded"
                              placeholder="I enjoy reading about..."></textarea>
                </div>

                <button type="submit"
                        class="bg-orange-500 text-white px-6 py-2 rounded">
                    Create Profile
                </button>
            </form>

            <div x-show="status === 'creating'" class="mt-4">
                <div class="flex items-center">
                    <svg class="animate-spin h-5 w-5 mr-3" viewBox="0 0 24 24">
                        <!-- spinner SVG -->
                    </svg>
                    <span>Generating your character profile...</span>
                </div>
            </div>

            <div x-show="status === 'error'" class="mt-4 text-red-600">
                <p x-text="errorMsg"></p>
                <button @click="retry()" class="mt-2 underline">Retry</button>
            </div>
        </div>
    }
}

<script>
function profileSetup() {
    return {
        status: 'idle',  // idle, creating, polling, ready, error
        errorMsg: '',
        profileId: null,

        handleResponse(event) {
            const response = JSON.parse(event.detail.xhr.response);
            this.profileId = response.id;
            this.status = 'polling';
            this.pollStatus();
        },

        pollStatus() {
            fetch(`/api/profiles/${this.profileId}`)
                .then(r => r.json())
                .then(profile => {
                    if (profile.character_status === 'ready') {
                        this.status = 'ready';
                        // Set cookie
                        document.cookie = `current_profile_id=${this.profileId}; path=/; max-age=2592000`;
                        window.location.href = '/';
                    } else if (profile.character_status === 'error') {
                        this.status = 'error';
                        this.errorMsg = profile.character_error || 'Unknown error';
                    } else {
                        // Still pending/updating, poll again
                        setTimeout(() => this.pollStatus(), 2000);
                    }
                });
        },

        retry() {
            // Re-trigger character generation
            fetch(`/api/profiles/${this.profileId}`, {
                method: 'PATCH',
                headers: {'Content-Type': 'application/json'},
                body: JSON.stringify({})
            }).then(() => {
                this.status = 'polling';
                this.pollStatus();
            });
        }
    }
}
</script>
```

### 7. Profile Switcher in Header
**File**: `front/templates/layouts/header.templ`

**Features**:
- Alpine.js dropdown (Netflix-style)
- Show current profile nickname
- List all profiles
- "Manage Profiles" link at bottom
- Switch endpoint sets cookie + reloads

**Example**:
```templ
<div x-data="{ open: false }" class="relative">
    <button @click="open = !open" class="flex items-center gap-2">
        <span>{ currentProfile.Nickname }</span>
        <svg class="w-4 h-4" fill="currentColor" viewBox="0 0 20 20">
            <path d="M5.293 7.293a1 1 0 011.414 0L10 10.586l3.293-3.293a1 1 0 111.414 1.414l-4 4a1 1 0 01-1.414 0l-4-4a1 1 0 010-1.414z"/>
        </svg>
    </button>

    <div x-show="open"
         @click.away="open = false"
         class="absolute right-0 mt-2 w-48 bg-white rounded-lg shadow-lg">

        for _, profile := range profiles {
            <a href={"/profiles/switch/" + profile.ID}
               class="block px-4 py-2 hover:bg-gray-100">
                { profile.Nickname }
            </a>
        }

        <hr class="my-2"/>

        <a href="/profiles/manage"
           class="block px-4 py-2 hover:bg-gray-100 text-sm">
            Manage Profiles
        </a>
    </div>
</div>
```

**Handler for profile switch**:
```go
// In front handlers
func (h *Handler) SwitchProfile(w http.ResponseWriter, r *http.Request) {
    profileID := chi.URLParam(r, "id")

    // Set cookie
    http.SetCookie(w, &http.Cookie{
        Name:     "current_profile_id",
        Value:    profileID,
        Path:     "/",
        MaxAge:   30 * 24 * 60 * 60, // 30 days
        HttpOnly: true,
        SameSite: http.SameSiteLaxMode,
    })

    // Redirect to home
    http.Redirect(w, r, "/", http.StatusFound)
}
```

### 8. Like Button Component
**File**: `front/templates/components/like-button.templ`

**Features**:
- Shows ❤️ (filled) when liked, 🤍 (outline) when not
- HTMX toggle with optimistic swap
- Sends profile_id from cookie/context

**Example**:
```templ
package components

templ LikeButton(articleID string, liked bool, likeID string) {
    if liked {
        <button
            hx-delete={"/api/likes/" + likeID}
            hx-swap="outerHTML"
            hx-vals='{"profile_id": getProfileID()}'
            class="like-btn liked">
            <span class="text-red-500">❤️</span>
            <span>Liked</span>
        </button>
    } else {
        <button
            hx-post={"/api/articles/" + articleID + "/like"}
            hx-swap="outerHTML"
            hx-vals='{"profile_id": getProfileID()}'
            class="like-btn">
            <span>🤍</span>
            <span>Like</span>
        </button>
    }
}
```

**Add to NewsItem**:
```templ
// In front/templates/components/news-item.templ
templ NewsItem(article models.Article, liked bool, likeID string) {
    <article class="...">
        <div>{article.Title}</div>
        <div class="metadata">
            {article.TimeAgo}
            @LikeButton(article.ID, liked, likeID)
        </div>
    </article>
}
```

### 9. Profile Management Page
**File**: `front/templates/pages/profiles.templ`

**Features**:
- List all profiles
- Edit form per profile (nickname, user_description)
- Character displayed as readonly
- Status indicator
- Delete with confirmation

---

## 🧪 TESTING CHECKLIST

### Backend API Testing

```bash
# 1. Create profile
curl -X POST http://localhost:8080/profiles \
  -H "Content-Type: application/json" \
  -d '{"nickname": "Tech Fan", "user_description": "I love Go and distributed systems"}'

# Expected: Returns profile with character_status="pending"
# Wait 5-10 seconds for character generation

# 2. Poll profile status
curl http://localhost:8080/profiles/{profile_id}
# Expected: character_status changes to "ready" and character field populated

# 3. Create source (with profile_id)
curl -X POST http://localhost:8080/sources \
  -H "Content-Type: application/json" \
  -d '{
    "profile_id": "{profile_id}",
    "type": "reddit",
    "config": {"subreddit": "golang", "sort": "hot", "limit": 10}
  }'

# 4. Like an article
curl -X POST http://localhost:8080/articles/{article_id}/like \
  -H "Content-Type: application/json" \
  -d '{"profile_id": "{profile_id}"}'

# 5. Check milestone updates
# Like 3 articles -> milestone should become "3", character updates
# Like 10 articles -> milestone becomes "10"
# Like 20 articles -> milestone becomes "20"
# Like 21st article -> milestone becomes "weekly"

# 6. Test weekly cron (if implemented)
# Wait until next Monday 01:00 AM or manually trigger scheduler
```

### Frontend Testing
- [ ] Navigate to app without profiles → redirects to /profiles/setup
- [ ] Create profile → shows loading spinner → redirects to home when ready
- [ ] Click profile dropdown → shows all profiles
- [ ] Switch profile → page reloads with new profile's data
- [ ] Click like button → instantly shows as liked
- [ ] Unlike → reverts to unliked state

---

## 🚨 KNOWN ISSUES & GOTCHAS

### 1. Gemini API Key Validation
**Issue**: App doesn't validate `GEMINI_API_KEY` at startup
**Impact**: Service runs but profile creation fails with cryptic error
**Fix**: Add validation in `main.go`:
```go
if cfg.Collector.Gemini.APIKey == "" {
    log.Fatalf("GEMINI_API_KEY is required for profile feature")
}
```

### 2. Error Matching with Strings
**Issue**: `isLikeUniqueViolation()` uses string matching instead of typed errors
**Impact**: Brittle, may break with SQLite version changes
**Better Approach**:
```go
import "github.com/ncruces/go-sqlite3"

func isLikeUniqueViolation(err error) bool {
    if sqliteErr, ok := err.(sqlite3.Error); ok {
        return sqliteErr.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE
    }
    return false
}
```

### 3. Sources Without Profile ID (Legacy Data)
**Issue**: Existing sources in DB don't have profile_id
**Impact**: Scheduler will fail to fetch articles for old sources
**Migration Needed**: Either clear DB or add default profile and UPDATE sources

### 4. Article Duplication Per Profile
**Decision**: User chose to duplicate articles per profile for isolation
**Trade-off**: More storage, but simpler queries
**Alternative**: Could use junction table in future refactor

### 5. Concurrent UpdateCharacter Calls
**Issue**: No mutex prevents multiple simultaneous updates for same profile
**Impact**: Could waste Gemini API quota
**Optional Fix**: Add sync.Map to track active updates in UpdateService

---

## 📝 ENVIRONMENT VARIABLES REFERENCE

Required for profile feature:

```bash
# Gemini API
GEMINI_API_KEY="your-api-key-here"          # Required

# Profile updates
PROFILE_DAILY_CRON="0 1 * * *"              # Daily at 1 AM (default)
# For Monday-only: "0 1 * * 1"

# Existing config (still needed)
COLLECTOR_PORT=8080
COLLECTOR_DB_PATH=/data/meows.db
COLLECTOR_REDDIT_CLIENT_ID="..."
COLLECTOR_REDDIT_CLIENT_SECRET="..."
COLLECTOR_REDDIT_USERNAME="..."
COLLECTOR_REDDIT_PASSWORD="..."
COLLECTOR_SEMANTIC_SCHOLAR_API_KEY="..."
```

---

## 🔄 SESSION RESET RECOVERY

If starting fresh:

1. **Check what's already in database**:
   ```sql
   -- Check if schema exists
   SELECT name FROM sqlite_master WHERE type='table' AND name='profiles';

   -- If exists, check data
   SELECT * FROM profiles;
   SELECT COUNT(*) FROM likes;
   ```

2. **Verify Go packages installed**:
   ```bash
   cd collector
   go mod download
   go build ./cmd/server
   ```

3. **Check if Gemini is working**:
   ```bash
   # Test token counting
   go test ./internal/gemini -v
   ```

4. **Start with critical fixes**:
   - Fix LikeArticle race condition (transaction)
   - Implement weekly cron
   - Then move to frontend

---

## 📚 REFERENCES

- **Original Plan**: Session from 2025-11-18
- **Gemini API Docs**: Context7 library ID `/googleapis/go-genai`
- **Key Files Modified**:
  - `collector/internal/db/db.go` - Schema
  - `collector/internal/db/models.go` - Models
  - `collector/internal/gemini/gemini.go` - Gemini client
  - `collector/internal/profile/profile_update.go` - Business logic
  - `collector/internal/api/handlers.go` - Endpoints
  - `collector/internal/api/router.go` - Routes
  - `collector/internal/config/config.go` - Config
  - `collector/cmd/server/main.go` - Initialization

**Last Updated**: 2025-11-18
**Completion**: ~60% backend, 0% frontend
