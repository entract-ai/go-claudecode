package claudecode

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

// SDKSessionInfo contains session metadata returned by ListSessions.
//
// Contains only data extractable from stat + head/tail reads -- no full
// JSONL parsing required.
type SDKSessionInfo struct {
	// SessionID is the unique session identifier (UUID).
	SessionID string

	// Summary is the display title for the session -- custom title,
	// auto-generated summary, or first prompt.
	Summary string

	// LastModified is the last modified time in milliseconds since epoch.
	LastModified int64

	// FileSize is the session file size in bytes. Only populated for local
	// JSONL storage; may be nil for remote storage backends.
	FileSize *int64

	// CustomTitle is the user-set custom title or AI-generated title.
	// Nil when no title has been set.
	CustomTitle *string

	// FirstPrompt is the first meaningful user prompt in the session.
	// Nil when no user prompt was found.
	FirstPrompt *string

	// GitBranch is the git branch at the end of the session.
	// Nil when git info is unavailable.
	GitBranch *string

	// Cwd is the working directory for the session.
	// Nil when the working directory is unknown.
	Cwd *string

	// Tag is the user-set session tag.
	// Nil when no tag has been set.
	Tag *string

	// CreatedAt is the creation time in milliseconds since epoch, extracted
	// from the first entry's ISO timestamp field. Nil when no timestamp
	// is available.
	CreatedAt *int64
}

// SessionMessage is a user or assistant message from a session transcript.
//
// Returned by GetSessionMessages for reading historical session data.
type SessionMessage struct {
	// Type is the message type: "user" or "assistant".
	Type string

	// UUID is the unique message identifier.
	UUID string

	// SessionID is the ID of the session this message belongs to.
	SessionID string

	// Message is the raw Anthropic API message (role, content, etc.).
	Message map[string]any
}

// ---------------------------------------------------------------------------
// Options
// ---------------------------------------------------------------------------

// sessionOptions holds configuration for session listing/reading/mutation functions.
type sessionOptions struct {
	directory        string
	limit            int    // 0 = no limit
	offset           int
	includeWorktrees *bool  // nil = default (true for list, n/a for messages)
	configDir        string // test override

	// Fork-specific options
	upToMessageID string
	forkTitle     string
}

// SessionOption configures ListSessions or GetSessionMessages.
type SessionOption func(*sessionOptions)

// WithSessionDirectory sets the project directory to scope the search.
// When omitted, all project directories are searched.
func WithSessionDirectory(dir string) SessionOption {
	return func(o *sessionOptions) {
		o.directory = dir
	}
}

// WithSessionLimit sets the maximum number of sessions to return.
// A value of 0 or negative means no limit.
func WithSessionLimit(n int) SessionOption {
	return func(o *sessionOptions) {
		o.limit = n
	}
}

// WithSessionOffset sets the number of sessions to skip (for pagination).
func WithSessionOffset(n int) SessionOption {
	return func(o *sessionOptions) {
		o.offset = n
	}
}

// WithIncludeWorktrees controls whether git worktree paths are included
// when listing sessions for a specific directory. Defaults to true.
func WithIncludeWorktrees(include bool) SessionOption {
	return func(o *sessionOptions) {
		o.includeWorktrees = &include
	}
}

// WithForkUpToMessageID slices the fork transcript up to (and including)
// the specified message UUID. If omitted, copies the full transcript.
func WithForkUpToMessageID(messageID string) SessionOption {
	return func(o *sessionOptions) {
		o.upToMessageID = messageID
	}
}

// WithForkTitle sets a custom title for the forked session. If omitted,
// derives the title from the original session title + " (fork)".
func WithForkTitle(title string) SessionOption {
	return func(o *sessionOptions) {
		o.forkTitle = title
	}
}

// WithMessageLimit sets the maximum number of messages to return.
// A value of 0 or negative means no limit.
func WithMessageLimit(n int) SessionOption {
	return func(o *sessionOptions) {
		o.limit = n
	}
}

// WithMessageOffset sets the number of messages to skip (for pagination).
func WithMessageOffset(n int) SessionOption {
	return func(o *sessionOptions) {
		o.offset = n
	}
}

func applySessionOptions(opts ...SessionOption) *sessionOptions {
	o := &sessionOptions{}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	// liteReadBufSize is the size of the head/tail buffer for metadata reads.
	liteReadBufSize = 65536

	// maxSanitizedLength is the maximum length for a sanitized path component.
	maxSanitizedLength = 200
)

var (
	uuidRE = regexp.MustCompile(
		`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`,
	)
	sanitizeRE = regexp.MustCompile(`[^a-zA-Z0-9]`)

	skipFirstPromptPattern = regexp.MustCompile(
		`^(?:<local-command-stdout>|<session-start-hook>|<tick>|<goal>|` +
			`\[Request interrupted by user[^\]]*\]|` +
			`\s*<ide_opened_file>[\s\S]*</ide_opened_file>\s*$|` +
			`\s*<ide_selection>[\s\S]*</ide_selection>\s*$)`,
	)
	commandNameRE = regexp.MustCompile(`<command-name>(.*?)</command-name>`)
)

// Transcript entry types that carry uuid + parentUuid chain links.
var transcriptEntryTypes = map[string]bool{
	"user": true, "assistant": true, "progress": true,
	"system": true, "attachment": true,
}

// ---------------------------------------------------------------------------
// UUID validation
// ---------------------------------------------------------------------------

func validateUUID(s string) bool {
	return uuidRE.MatchString(s)
}

// ---------------------------------------------------------------------------
// Path sanitization
// ---------------------------------------------------------------------------

// simpleHash computes a 32-bit integer hash to base36, matching the CLI's
// directory naming scheme.
func simpleHash(s string) string {
	var h int64
	for _, ch := range s {
		h = (h << 5) - h + int64(ch)
		// Emulate JS `hash |= 0` (coerce to 32-bit signed int)
		h = h & 0xFFFFFFFF
		if h >= 0x80000000 {
			h -= 0x100000000
		}
	}
	if h < 0 {
		h = -h
	}
	if h == 0 {
		return "0"
	}
	const digits = "0123456789abcdefghijklmnopqrstuvwxyz"
	var out []byte
	n := h
	for n > 0 {
		out = append(out, digits[n%36])
		n /= 36
	}
	// Reverse
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return string(out)
}

// sanitizePath makes a string safe for use as a directory name.
// Replaces all non-alphanumeric characters with hyphens. For paths
// exceeding maxSanitizedLength, truncates and appends a hash suffix.
func sanitizePath(name string) string {
	sanitized := sanitizeRE.ReplaceAllString(name, "-")
	if len(sanitized) <= maxSanitizedLength {
		return sanitized
	}
	h := simpleHash(name)
	return sanitized[:maxSanitizedLength] + "-" + h
}

// ---------------------------------------------------------------------------
// Config directories
// ---------------------------------------------------------------------------

func getClaudeConfigDir(override string) string {
	if override != "" {
		return override
	}
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude")
}

func getProjectsDir(configDir string) string {
	return filepath.Join(configDir, "projects")
}

func getProjectDir(configDir, projectPath string) string {
	return filepath.Join(getProjectsDir(configDir), sanitizePath(projectPath))
}

func canonicalizePath(d string) string {
	resolved, err := filepath.EvalSymlinks(d)
	if err != nil {
		return d
	}
	abs, err := filepath.Abs(resolved)
	if err != nil {
		return resolved
	}
	return abs
}

// findProjectDir finds the project directory for a given path, tolerating
// hash mismatches for long paths.
func findProjectDir(configDir, projectPath string) string {
	exact := getProjectDir(configDir, projectPath)
	info, err := os.Stat(exact)
	if err == nil && info.IsDir() {
		return exact
	}

	// For short paths, exact match is all we try.
	sanitized := sanitizePath(projectPath)
	if len(sanitized) <= maxSanitizedLength {
		return ""
	}

	// For long paths, try prefix matching to handle hash mismatches.
	prefix := sanitized[:maxSanitizedLength]
	projectsDir := getProjectsDir(configDir)
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), prefix+"-") {
			return filepath.Join(projectsDir, entry.Name())
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// JSON string field extraction -- no full parse, works on truncated lines
// ---------------------------------------------------------------------------

func unescapeJSONString(raw string) string {
	if !strings.Contains(raw, `\`) {
		return raw
	}
	var result string
	if err := json.Unmarshal([]byte(`"`+raw+`"`), &result); err != nil {
		return raw
	}
	return result
}

// extractJSONStringField extracts a simple JSON string field value without
// full parsing. Looks for "key":"value" or "key": "value" patterns.
// Returns the first match.
func extractJSONStringField(text, key string) (string, bool) {
	patterns := []string{
		`"` + key + `":"`,
		`"` + key + `": "`,
	}
	for _, pattern := range patterns {
		idx := strings.Index(text, pattern)
		if idx < 0 {
			continue
		}
		valueStart := idx + len(pattern)
		i := valueStart
		for i < len(text) {
			if text[i] == '\\' {
				i += 2
				continue
			}
			if text[i] == '"' {
				return unescapeJSONString(text[valueStart:i]), true
			}
			i++
		}
	}
	return "", false
}

// extractLastJSONStringField finds the LAST occurrence of a JSON string field.
func extractLastJSONStringField(text, key string) (string, bool) {
	patterns := []string{
		`"` + key + `":"`,
		`"` + key + `": "`,
	}
	var lastValue string
	found := false
	for _, pattern := range patterns {
		searchFrom := 0
		for {
			idx := strings.Index(text[searchFrom:], pattern)
			if idx < 0 {
				break
			}
			idx += searchFrom
			valueStart := idx + len(pattern)
			i := valueStart
			for i < len(text) {
				if text[i] == '\\' {
					i += 2
					continue
				}
				if text[i] == '"' {
					lastValue = unescapeJSONString(text[valueStart:i])
					found = true
					break
				}
				i++
			}
			searchFrom = i + 1
		}
	}
	return lastValue, found
}

// ---------------------------------------------------------------------------
// First prompt extraction from head chunk
// ---------------------------------------------------------------------------

func extractFirstPromptFromHead(head string) string {
	start := 0
	commandFallback := ""
	headLen := len(head)

	for start < headLen {
		newlineIdx := strings.Index(head[start:], "\n")
		var line string
		if newlineIdx >= 0 {
			line = head[start : start+newlineIdx]
			start = start + newlineIdx + 1
		} else {
			line = head[start:]
			start = headLen
		}

		if !strings.Contains(line, `"type":"user"`) && !strings.Contains(line, `"type": "user"`) {
			continue
		}
		if strings.Contains(line, `"tool_result"`) {
			continue
		}
		if strings.Contains(line, `"isMeta":true`) || strings.Contains(line, `"isMeta": true`) {
			continue
		}
		if strings.Contains(line, `"isCompactSummary":true`) || strings.Contains(line, `"isCompactSummary": true`) {
			continue
		}

		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if entryType, _ := entry["type"].(string); entryType != "user" {
			continue
		}

		message, ok := entry["message"].(map[string]any)
		if !ok {
			continue
		}

		var texts []string
		switch content := message["content"].(type) {
		case string:
			texts = append(texts, content)
		case []any:
			for _, block := range content {
				bm, ok := block.(map[string]any)
				if !ok {
					continue
				}
				if bm["type"] == "text" {
					if t, ok := bm["text"].(string); ok {
						texts = append(texts, t)
					}
				}
			}
		}

		for _, raw := range texts {
			result := strings.TrimSpace(strings.ReplaceAll(raw, "\n", " "))
			if result == "" {
				continue
			}

			// Skip slash-command messages but remember first as fallback
			if cmdMatch := commandNameRE.FindStringSubmatch(result); len(cmdMatch) > 1 {
				if commandFallback == "" {
					commandFallback = cmdMatch[1]
				}
				continue
			}

			if skipFirstPromptPattern.MatchString(result) {
				continue
			}

			if len([]rune(result)) > 200 {
				runes := []rune(result)
				result = strings.TrimRightFunc(string(runes[:200]), unicode.IsSpace) + "\u2026"
			}
			return result
		}
	}

	if commandFallback != "" {
		return commandFallback
	}
	return ""
}

// ---------------------------------------------------------------------------
// File I/O -- read head and tail of a file
// ---------------------------------------------------------------------------

type liteSessionFile struct {
	mtime int64 // milliseconds since epoch
	size  int64
	head  string
	tail  string
}

// readSessionLite opens a session file, stats it, and reads head + tail.
// Returns nil on any error or if file is empty.
func readSessionLite(filePath string) *liteSessionFile {
	f, err := os.Open(filePath)
	if err != nil {
		return nil
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return nil
	}
	size := stat.Size()
	mtime := stat.ModTime().UnixMilli()

	headBuf := make([]byte, liteReadBufSize)
	n, err := f.Read(headBuf)
	if n == 0 || (err != nil && err != io.EOF) {
		return nil
	}
	head := string(headBuf[:n])

	var tail string
	tailOffset := size - liteReadBufSize
	if tailOffset <= 0 {
		tail = head
	} else {
		if _, err := f.Seek(tailOffset, io.SeekStart); err != nil {
			return nil
		}
		tailBuf := make([]byte, liteReadBufSize)
		tn, err := f.Read(tailBuf)
		if tn == 0 || (err != nil && err != io.EOF) {
			return nil
		}
		tail = string(tailBuf[:tn])
	}

	return &liteSessionFile{mtime: mtime, size: size, head: head, tail: tail}
}

// ---------------------------------------------------------------------------
// Field extraction
// ---------------------------------------------------------------------------

// parseSessionInfoFromLite parses SDKSessionInfo fields from a lite session read.
// Returns nil for sidechain sessions or metadata-only sessions with no
// extractable summary.
func parseSessionInfoFromLite(sessionID string, lite *liteSessionFile, projectPath string) *SDKSessionInfo {
	head, tail := lite.head, lite.tail

	// Check first line for sidechain sessions
	firstNewline := strings.Index(head, "\n")
	var firstLine string
	if firstNewline >= 0 {
		firstLine = head[:firstNewline]
	} else {
		firstLine = head
	}
	if strings.Contains(firstLine, `"isSidechain":true`) || strings.Contains(firstLine, `"isSidechain": true`) {
		return nil
	}

	// User-set title (customTitle) wins over AI-generated title (aiTitle).
	// Head fallback covers short sessions where the title entry may not be in tail.
	var customTitle *string
	if v, ok := extractLastJSONStringField(tail, "customTitle"); ok && v != "" {
		customTitle = &v
	} else if v, ok := extractLastJSONStringField(head, "customTitle"); ok && v != "" {
		customTitle = &v
	} else if v, ok := extractLastJSONStringField(tail, "aiTitle"); ok && v != "" {
		customTitle = &v
	} else if v, ok := extractLastJSONStringField(head, "aiTitle"); ok && v != "" {
		customTitle = &v
	}

	var firstPrompt *string
	if fp := extractFirstPromptFromHead(head); fp != "" {
		firstPrompt = &fp
	}

	// lastPrompt tail entry shows what the user was most recently doing
	var summary string
	if customTitle != nil {
		summary = *customTitle
	} else if v, ok := extractLastJSONStringField(tail, "lastPrompt"); ok && v != "" {
		summary = v
	} else if v, ok := extractLastJSONStringField(tail, "summary"); ok && v != "" {
		summary = v
	} else if firstPrompt != nil {
		summary = *firstPrompt
	}

	// Skip metadata-only sessions
	if summary == "" {
		return nil
	}

	var gitBranch *string
	if v, ok := extractLastJSONStringField(tail, "gitBranch"); ok && v != "" {
		gitBranch = &v
	} else if v, ok := extractJSONStringField(head, "gitBranch"); ok && v != "" {
		gitBranch = &v
	}

	var cwd *string
	if v, ok := extractJSONStringField(head, "cwd"); ok && v != "" {
		cwd = &v
	} else if projectPath != "" {
		cwd = &projectPath
	}

	// Scope tag extraction to {"type":"tag"} lines
	var tag *string
	tailLines := strings.Split(tail, "\n")
	for i := len(tailLines) - 1; i >= 0; i-- {
		ln := tailLines[i]
		if strings.HasPrefix(ln, `{"type":"tag"`) {
			if v, ok := extractLastJSONStringField(ln, "tag"); ok && v != "" {
				tag = &v
			}
			break
		}
	}

	// created_at from first entry's ISO timestamp
	var createdAt *int64
	if ts, ok := extractJSONStringField(firstLine, "timestamp"); ok {
		if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
			ms := t.UnixMilli()
			createdAt = &ms
		} else if strings.HasSuffix(ts, "Z") {
			// Try without the Z
			tsNoZ := ts[:len(ts)-1] + "+00:00"
			if t, err := time.Parse(time.RFC3339Nano, tsNoZ); err == nil {
				ms := t.UnixMilli()
				createdAt = &ms
			}
		}
	}

	return &SDKSessionInfo{
		SessionID:    sessionID,
		Summary:      summary,
		LastModified: lite.mtime,
		FileSize:     &lite.size,
		CustomTitle:  customTitle,
		FirstPrompt:  firstPrompt,
		GitBranch:    gitBranch,
		Cwd:          cwd,
		Tag:          tag,
		CreatedAt:    createdAt,
	}
}

// ---------------------------------------------------------------------------
// Core list implementation
// ---------------------------------------------------------------------------

func readSessionsFromDir(projectDir, projectPath string) []SDKSessionInfo {
	entries, err := os.ReadDir(projectDir)
	if err != nil {
		return nil
	}

	var results []SDKSessionInfo
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		sessionID := strings.TrimSuffix(name, ".jsonl")
		if !validateUUID(sessionID) {
			continue
		}

		lite := readSessionLite(filepath.Join(projectDir, name))
		if lite == nil {
			continue
		}

		info := parseSessionInfoFromLite(sessionID, lite, projectPath)
		if info != nil {
			results = append(results, *info)
		}
	}
	return results
}

func deduplicateBySessionID(sessions []SDKSessionInfo) []SDKSessionInfo {
	byID := make(map[string]SDKSessionInfo)
	for _, s := range sessions {
		if existing, ok := byID[s.SessionID]; !ok || s.LastModified > existing.LastModified {
			byID[s.SessionID] = s
		}
	}
	result := make([]SDKSessionInfo, 0, len(byID))
	for _, s := range byID {
		result = append(result, s)
	}
	return result
}

func applySortLimitOffset(sessions []SDKSessionInfo, limit, offset int) []SDKSessionInfo {
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].LastModified > sessions[j].LastModified
	})
	if offset > 0 {
		if offset >= len(sessions) {
			return nil
		}
		sessions = sessions[offset:]
	}
	if limit > 0 && limit < len(sessions) {
		sessions = sessions[:limit]
	}
	return sessions
}

// ListSessions lists sessions with metadata extracted from stat + head/tail reads.
//
// When a directory is provided via WithSessionDirectory, returns sessions for
// that project directory (and optionally its git worktrees). When omitted,
// returns sessions across all projects.
//
// Use WithSessionLimit and WithSessionOffset for pagination.
// Results are sorted by LastModified descending.
func ListSessions(opts ...SessionOption) ([]SDKSessionInfo, error) {
	o := applySessionOptions(opts...)
	configDir := getClaudeConfigDir(o.configDir)

	if o.directory != "" {
		return listSessionsForProject(configDir, o)
	}
	return listAllSessions(configDir, o)
}

func listSessionsForProject(configDir string, o *sessionOptions) ([]SDKSessionInfo, error) {
	canonicalDir := canonicalizePath(o.directory)

	includeWorktrees := true
	if o.includeWorktrees != nil {
		includeWorktrees = *o.includeWorktrees
	}

	if !includeWorktrees {
		projDir := findProjectDir(configDir, canonicalDir)
		if projDir == "" {
			return nil, nil
		}
		sessions := readSessionsFromDir(projDir, canonicalDir)
		return applySortLimitOffset(sessions, o.limit, o.offset), nil
	}

	// Worktree-aware scanning: find all project dirs matching any worktree
	worktreePaths := getWorktreePaths(canonicalDir)

	if len(worktreePaths) <= 1 {
		projDir := findProjectDir(configDir, canonicalDir)
		if projDir == "" {
			return nil, nil
		}
		sessions := readSessionsFromDir(projDir, canonicalDir)
		return applySortLimitOffset(sessions, o.limit, o.offset), nil
	}

	projectsDir := getProjectsDir(configDir)
	allDirents, err := os.ReadDir(projectsDir)
	if err != nil {
		// Fall back to single project dir
		projDir := findProjectDir(configDir, canonicalDir)
		if projDir == "" {
			return applySortLimitOffset(nil, o.limit, o.offset), nil
		}
		sessions := readSessionsFromDir(projDir, canonicalDir)
		return applySortLimitOffset(sessions, o.limit, o.offset), nil
	}

	type indexedWT struct {
		path   string
		prefix string
	}
	indexed := make([]indexedWT, 0, len(worktreePaths))
	for _, wt := range worktreePaths {
		sanitized := sanitizePath(wt)
		indexed = append(indexed, indexedWT{path: wt, prefix: sanitized})
	}
	// Sort by prefix length descending (longer/more specific first)
	sort.Slice(indexed, func(i, j int) bool {
		return len(indexed[i].prefix) > len(indexed[j].prefix)
	})

	var allSessions []SDKSessionInfo
	seenDirs := make(map[string]bool)

	// Always include the user's actual directory
	canonicalProjectDir := findProjectDir(configDir, canonicalDir)
	if canonicalProjectDir != "" {
		dirBase := filepath.Base(canonicalProjectDir)
		seenDirs[dirBase] = true
		sessions := readSessionsFromDir(canonicalProjectDir, canonicalDir)
		allSessions = append(allSessions, sessions...)
	}

	for _, entry := range allDirents {
		if !entry.IsDir() {
			continue
		}
		dirName := entry.Name()
		if seenDirs[dirName] {
			continue
		}

		for _, iwt := range indexed {
			isMatch := dirName == iwt.prefix || (len(iwt.prefix) >= maxSanitizedLength && strings.HasPrefix(dirName, iwt.prefix+"-"))
			if isMatch {
				seenDirs[dirName] = true
				sessions := readSessionsFromDir(filepath.Join(projectsDir, dirName), iwt.path)
				allSessions = append(allSessions, sessions...)
				break
			}
		}
	}

	deduped := deduplicateBySessionID(allSessions)
	return applySortLimitOffset(deduped, o.limit, o.offset), nil
}

func listAllSessions(configDir string, o *sessionOptions) ([]SDKSessionInfo, error) {
	projectsDir := getProjectsDir(configDir)
	dirs, err := os.ReadDir(projectsDir)
	if err != nil {
		return nil, nil
	}

	var allSessions []SDKSessionInfo
	for _, dir := range dirs {
		if !dir.IsDir() {
			continue
		}
		sessions := readSessionsFromDir(filepath.Join(projectsDir, dir.Name()), "")
		allSessions = append(allSessions, sessions...)
	}

	deduped := deduplicateBySessionID(allSessions)
	return applySortLimitOffset(deduped, o.limit, o.offset), nil
}

// ---------------------------------------------------------------------------
// GetSessionInfo -- single-session metadata lookup
// ---------------------------------------------------------------------------

// GetSessionInfo reads metadata for a single session by ID.
//
// Wraps readSessionLite for one file -- no O(n) directory scan.
// Directory resolution matches GetSessionMessages: the directory option is
// the project path; when omitted, all project directories are searched for
// the session file.
//
// Returns (nil, nil) if the session file is not found, the sessionID is not
// a valid UUID, or the session is a sidechain/metadata-only session with no
// extractable summary.
func GetSessionInfo(sessionID string, opts ...SessionOption) (*SDKSessionInfo, error) {
	if !validateUUID(sessionID) {
		return nil, nil
	}
	fileName := sessionID + ".jsonl"

	o := applySessionOptions(opts...)
	configDir := getClaudeConfigDir(o.configDir)

	if o.directory != "" {
		canonicalDir := canonicalizePath(o.directory)
		projDir := findProjectDir(configDir, canonicalDir)
		if projDir != "" {
			lite := readSessionLite(filepath.Join(projDir, fileName))
			if lite != nil {
				return parseSessionInfoFromLite(sessionID, lite, canonicalDir), nil
			}
		}

		// Worktree fallback -- matches GetSessionMessages semantics.
		// Sessions may live under a different worktree root.
		worktreePaths := getWorktreePaths(canonicalDir)
		for _, wt := range worktreePaths {
			if wt == canonicalDir {
				continue
			}
			wtProjDir := findProjectDir(configDir, wt)
			if wtProjDir != "" {
				lite := readSessionLite(filepath.Join(wtProjDir, fileName))
				if lite != nil {
					return parseSessionInfoFromLite(sessionID, lite, wt), nil
				}
			}
		}

		return nil, nil
	}

	// No directory -- search all project directories for the session file.
	projectsDir := getProjectsDir(configDir)
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return nil, nil
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		lite := readSessionLite(filepath.Join(projectsDir, entry.Name(), fileName))
		if lite != nil {
			return parseSessionInfoFromLite(sessionID, lite, ""), nil
		}
	}
	return nil, nil
}

// ---------------------------------------------------------------------------
// Git worktree detection
// ---------------------------------------------------------------------------

func getWorktreePaths(cwd string) []string {
	// We don't shell out to git here by default.
	// This could be implemented later if needed.
	// For now, return empty to indicate no worktrees detected.
	return nil
}

// ---------------------------------------------------------------------------
// GetSessionMessages -- full transcript reconstruction
// ---------------------------------------------------------------------------

// transcriptEntry is the internal type for parsed JSONL transcript entries.
type transcriptEntry struct {
	Type             string         `json:"type"`
	UUID             string         `json:"uuid"`
	ParentUUID       string         `json:"parentUuid"`
	SessionID        string         `json:"sessionId"`
	Message          map[string]any `json:"message"`
	IsSidechain      bool           `json:"isSidechain"`
	IsMeta           bool           `json:"isMeta"`
	IsCompactSummary bool           `json:"isCompactSummary"`
	TeamName         string         `json:"teamName"`
}

func readSessionFile(sessionID, configDir, directory string) string {
	fileName := sessionID + ".jsonl"

	if directory != "" {
		canonicalDir := canonicalizePath(directory)

		// Try the exact/prefix-matched project directory first
		projDir := findProjectDir(configDir, canonicalDir)
		if projDir != "" {
			content, err := os.ReadFile(filepath.Join(projDir, fileName))
			if err == nil && len(content) > 0 {
				return string(content)
			}
		}

		// Try worktree paths
		worktreePaths := getWorktreePaths(canonicalDir)
		for _, wt := range worktreePaths {
			if wt == canonicalDir {
				continue
			}
			wtProjDir := findProjectDir(configDir, wt)
			if wtProjDir != "" {
				content, err := os.ReadFile(filepath.Join(wtProjDir, fileName))
				if err == nil && len(content) > 0 {
					return string(content)
				}
			}
		}

		return ""
	}

	// No directory provided -- search all project directories
	projectsDir := getProjectsDir(configDir)
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		content, err := os.ReadFile(filepath.Join(projectsDir, entry.Name(), fileName))
		if err == nil && len(content) > 0 {
			return string(content)
		}
	}
	return ""
}

func parseTranscriptEntries(content string) []transcriptEntry {
	var entries []transcriptEntry
	start := 0
	length := len(content)

	for start < length {
		end := strings.Index(content[start:], "\n")
		if end == -1 {
			end = length - start
		}
		line := strings.TrimSpace(content[start : start+end])
		start = start + end + 1

		if line == "" {
			continue
		}

		var entry transcriptEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		if !transcriptEntryTypes[entry.Type] || entry.UUID == "" {
			continue
		}
		entries = append(entries, entry)
	}
	return entries
}

// buildConversationChain builds the conversation chain by finding the leaf
// and walking parentUuid. Returns messages in chronological order (root -> leaf).
func buildConversationChain(entries []transcriptEntry) []transcriptEntry {
	if len(entries) == 0 {
		return nil
	}

	// Index by uuid for O(1) parent lookup
	byUUID := make(map[string]*transcriptEntry, len(entries))
	for i := range entries {
		byUUID[entries[i].UUID] = &entries[i]
	}

	// Build index of entry positions (file order)
	entryIndex := make(map[string]int, len(entries))
	for i, e := range entries {
		entryIndex[e.UUID] = i
	}

	// Find terminal messages (no children point to them via parentUuid)
	parentUUIDs := make(map[string]bool)
	for _, e := range entries {
		if e.ParentUUID != "" {
			parentUUIDs[e.ParentUUID] = true
		}
	}

	var terminals []transcriptEntry
	for _, e := range entries {
		if !parentUUIDs[e.UUID] {
			terminals = append(terminals, e)
		}
	}

	// From each terminal, walk back to find the nearest user/assistant leaf
	var leaves []transcriptEntry
	for _, terminal := range terminals {
		cur := &terminal
		seen := make(map[string]bool)
		for cur != nil {
			if seen[cur.UUID] {
				break
			}
			seen[cur.UUID] = true
			if cur.Type == "user" || cur.Type == "assistant" {
				leaves = append(leaves, *cur)
				break
			}
			if cur.ParentUUID != "" {
				cur = byUUID[cur.ParentUUID]
			} else {
				cur = nil
			}
		}
	}

	if len(leaves) == 0 {
		return nil
	}

	// Pick the leaf from the main chain (not sidechain/team/meta), preferring
	// the highest position in the entries array
	var mainLeaves []transcriptEntry
	for _, leaf := range leaves {
		if !leaf.IsSidechain && leaf.TeamName == "" && !leaf.IsMeta {
			mainLeaves = append(mainLeaves, leaf)
		}
	}

	pickBest := func(candidates []transcriptEntry) transcriptEntry {
		best := candidates[0]
		bestIdx := entryIndex[best.UUID]
		for _, cur := range candidates[1:] {
			curIdx := entryIndex[cur.UUID]
			if curIdx > bestIdx {
				best = cur
				bestIdx = curIdx
			}
		}
		return best
	}

	var leaf transcriptEntry
	if len(mainLeaves) > 0 {
		leaf = pickBest(mainLeaves)
	} else {
		leaf = pickBest(leaves)
	}

	// Walk from leaf to root via parentUuid
	var chain []transcriptEntry
	chainSeen := make(map[string]bool)
	cur := &leaf
	for cur != nil {
		if chainSeen[cur.UUID] {
			break
		}
		chainSeen[cur.UUID] = true
		chain = append(chain, *cur)
		if cur.ParentUUID != "" {
			cur = byUUID[cur.ParentUUID]
		} else {
			cur = nil
		}
	}

	// Reverse to get root -> leaf order
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return chain
}

func isVisibleMessage(entry transcriptEntry) bool {
	if entry.Type != "user" && entry.Type != "assistant" {
		return false
	}
	if entry.IsMeta {
		return false
	}
	if entry.IsSidechain {
		return false
	}
	// isCompactSummary messages are intentionally included.
	return entry.TeamName == ""
}

func toSessionMessage(entry transcriptEntry) SessionMessage {
	msgType := "user"
	if entry.Type == "assistant" {
		msgType = "assistant"
	}
	return SessionMessage{
		Type:      msgType,
		UUID:      entry.UUID,
		SessionID: entry.SessionID,
		Message:   entry.Message,
	}
}

// GetSessionMessages reads a session's conversation messages from its JSONL
// transcript file.
//
// Parses the full JSONL, builds the conversation chain via parentUuid links,
// and returns user/assistant messages in chronological order.
//
// Returns an empty slice if the session is not found, the sessionID is not a
// valid UUID, or the transcript contains no visible messages.
func GetSessionMessages(sessionID string, opts ...SessionOption) ([]SessionMessage, error) {
	if !validateUUID(sessionID) {
		return nil, nil
	}

	o := applySessionOptions(opts...)
	configDir := getClaudeConfigDir(o.configDir)

	content := readSessionFile(sessionID, configDir, o.directory)
	if content == "" {
		return nil, nil
	}

	entries := parseTranscriptEntries(content)
	chain := buildConversationChain(entries)

	var messages []SessionMessage
	for _, e := range chain {
		if isVisibleMessage(e) {
			messages = append(messages, toSessionMessage(e))
		}
	}

	// Apply offset and limit
	if o.offset > 0 {
		if o.offset >= len(messages) {
			return nil, nil
		}
		messages = messages[o.offset:]
	}
	if o.limit > 0 && o.limit < len(messages) {
		messages = messages[:o.limit]
	}

	return messages, nil
}
