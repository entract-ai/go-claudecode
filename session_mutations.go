package claudecode

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// RenameSession renames a session by appending a custom-title JSONL entry.
//
// ListSessions reads the LAST custom-title from the file tail, so repeated
// calls are safe -- the most recent wins.
//
// The title is stripped of leading/trailing whitespace before storing.
// Returns an error if sessionID is not a valid UUID, if the title is
// empty/whitespace-only, or if the session file cannot be found.
//
// When a directory is provided via WithSessionDirectory, searches that
// project dir (and worktree fallback). When omitted, searches all project
// directories.
func RenameSession(sessionID, title string, opts ...SessionOption) error {
	if !validateUUID(sessionID) {
		return fmt.Errorf("invalid session ID: %s", sessionID)
	}

	stripped := strings.TrimSpace(title)
	if stripped == "" {
		return errors.New("title must be non-empty")
	}

	// Compact JSON with no spaces after separators, matching CLI format.
	data := `{"type":"custom-title","customTitle":` + compactJSONString(stripped) +
		`,"sessionId":"` + sessionID + `"}` + "\n"

	o := applySessionOptions(opts...)
	return appendToSession(sessionID, data, o)
}

// TagSession tags a session by appending a tag JSONL entry. Pass nil to
// clear the tag.
//
// ListSessions reads the LAST tag from the file tail, so repeated calls are
// safe -- the most recent wins. Passing nil appends an empty-string tag
// entry which ListSessions treats as cleared.
//
// Tags are Unicode-sanitized before storing (removes zero-width chars,
// directional marks, private-use characters, etc.) for CLI filter
// compatibility.
//
// Returns an error if sessionID is not a valid UUID, if the tag is
// empty/whitespace-only after sanitization (use nil to clear), or if the
// session file cannot be found.
func TagSession(sessionID string, tag *string, opts ...SessionOption) error {
	if !validateUUID(sessionID) {
		return fmt.Errorf("invalid session ID: %s", sessionID)
	}

	tagValue := ""
	if tag != nil {
		sanitized := strings.TrimSpace(sanitizeUnicode(*tag))
		if sanitized == "" {
			return errors.New("tag must be non-empty (use nil to clear)")
		}
		tagValue = sanitized
	}

	data := `{"type":"tag","tag":` + compactJSONString(tagValue) +
		`,"sessionId":"` + sessionID + `"}` + "\n"

	o := applySessionOptions(opts...)
	return appendToSession(sessionID, data, o)
}

// DeleteSession deletes a session by removing its JSONL file.
//
// This is a hard delete -- the file is removed permanently. SDK users who
// need soft-delete semantics can use TagSession(id, "__hidden") and filter
// on listing instead.
//
// Returns an error if sessionID is not a valid UUID or if the session file
// cannot be found.
//
// When a directory is provided via WithSessionDirectory, searches that
// project dir (and worktree fallback). When omitted, searches all project
// directories.
func DeleteSession(sessionID string, opts ...SessionOption) error {
	if !validateUUID(sessionID) {
		return fmt.Errorf("invalid session ID: %s", sessionID)
	}

	o := applySessionOptions(opts...)
	configDir := getClaudeConfigDir(o.configDir)

	path := findSessionFile(sessionID, configDir, o.directory)
	if path == "" {
		if o.directory != "" {
			return fmt.Errorf("session %s not found in project directory for %s", sessionID, o.directory)
		}
		return fmt.Errorf("session %s not found", sessionID)
	}

	if err := os.Remove(path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("session %s not found", sessionID)
		}
		return err
	}
	return nil
}

// findSessionFile finds the path to a session's JSONL file.
// Returns the file path if found, empty string otherwise.
func findSessionFile(sessionID, configDir, directory string) string {
	result, _ := findSessionFileWithDir(sessionID, configDir, directory)
	return result
}

// findSessionFileWithDir finds a session file and its containing project
// directory. Returns (filePath, projectDir) or ("", "") if not found. The
// fork operation needs the project dir to write the new file adjacent to
// the source.
func findSessionFileWithDir(sessionID, configDir, directory string) (string, string) {
	fileName := sessionID + ".jsonl"

	tryDir := func(projectDir string) (string, string) {
		path := filepath.Join(projectDir, fileName)
		info, err := os.Stat(path)
		if err != nil {
			return "", ""
		}
		if info.Size() > 0 {
			return path, projectDir
		}
		return "", ""
	}

	if directory != "" {
		canonical := canonicalizePath(directory)
		projDir := findProjectDir(configDir, canonical)
		if projDir != "" {
			if path, dir := tryDir(projDir); path != "" {
				return path, dir
			}
		}

		worktreePaths := getWorktreePaths(canonical)
		for _, wt := range worktreePaths {
			if wt == canonical {
				continue
			}
			wtProjDir := findProjectDir(configDir, wt)
			if wtProjDir != "" {
				if path, dir := tryDir(wtProjDir); path != "" {
					return path, dir
				}
			}
		}
		return "", ""
	}

	projectsDir := getProjectsDir(configDir)
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return "", ""
	}
	for _, entry := range entries {
		if path, dir := tryDir(filepath.Join(projectsDir, entry.Name())); path != "" {
			return path, dir
		}
	}
	return "", ""
}

// ---------------------------------------------------------------------------
// ForkSession
// ---------------------------------------------------------------------------

// ForkSessionResult contains the result of a fork operation.
type ForkSessionResult struct {
	// SessionID is the UUID of the new forked session.
	SessionID string
}

// ForkSession forks a session into a new branch with fresh UUIDs.
//
// Copies transcript messages from the source session into a new session file,
// remapping every message UUID and preserving the parentUuid chain. Supports
// slicing at a specific message via WithForkUpToMessageID.
//
// Forked sessions start without undo history (file-history snapshots are not
// copied).
//
// Returns an error if sessionID is not a valid UUID, if the source session
// file cannot be found, if the session has no messages to fork, or if
// upToMessageID is not found in the transcript.
func ForkSession(sessionID string, opts ...SessionOption) (*ForkSessionResult, error) {
	if !validateUUID(sessionID) {
		return nil, fmt.Errorf("invalid session ID: %s", sessionID)
	}

	o := applySessionOptions(opts...)
	if o.upToMessageID != "" && !validateUUID(o.upToMessageID) {
		return nil, fmt.Errorf("invalid up_to_message_id: %s", o.upToMessageID)
	}

	configDir := getClaudeConfigDir(o.configDir)
	filePath, projectDir := findSessionFileWithDir(sessionID, configDir, o.directory)
	if filePath == "" {
		if o.directory != "" {
			return nil, fmt.Errorf("session %s not found in project directory for %s", sessionID, o.directory)
		}
		return nil, fmt.Errorf("session %s not found", sessionID)
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	if len(content) == 0 {
		return nil, fmt.Errorf("session %s has no messages to fork", sessionID)
	}

	transcript, contentReplacements := parseForkTranscript(content, sessionID)

	// Filter out sidechains (subagent sessions with separate parentUuid
	// graphs). Keep isMeta entries -- they're interleaved in the main chain.
	filtered := transcript[:0]
	for _, e := range transcript {
		if isSidechain, _ := e["isSidechain"].(bool); !isSidechain {
			filtered = append(filtered, e)
		}
	}
	transcript = filtered

	if len(transcript) == 0 {
		return nil, fmt.Errorf("session %s has no messages to fork", sessionID)
	}

	if o.upToMessageID != "" {
		cutoff := -1
		for i, entry := range transcript {
			if uid, _ := entry["uuid"].(string); uid == o.upToMessageID {
				cutoff = i
				break
			}
		}
		if cutoff == -1 {
			return nil, fmt.Errorf("message %s not found in session %s", o.upToMessageID, sessionID)
		}
		transcript = transcript[:cutoff+1]
	}

	// Build UUID mapping. Include progress entries -- needed for parentUuid
	// chain walk.
	uuidMapping := make(map[string]string, len(transcript))
	for _, entry := range transcript {
		oldUUID, _ := entry["uuid"].(string)
		uuidMapping[oldUUID] = newUUID()
	}

	// Filter out progress messages from written output. They're UI-only
	// chain links; not needed in a fresh fork.
	var writable []map[string]any
	for _, e := range transcript {
		if entryType, _ := e["type"].(string); entryType != "progress" {
			writable = append(writable, e)
		}
	}
	if len(writable) == 0 {
		return nil, fmt.Errorf("session %s has no messages to fork", sessionID)
	}

	// Build by-UUID index for parent chain walking.
	byUUID := make(map[string]map[string]any, len(transcript))
	for _, entry := range transcript {
		uid, _ := entry["uuid"].(string)
		byUUID[uid] = entry
	}

	forkedSessionID := newUUID()
	now := time.Now().UTC().Format(time.RFC3339)
	// Replace +00:00 with Z if present (Go outputs Z for UTC already,
	// but be explicit).
	now = strings.Replace(now, "+00:00", "Z", 1)

	var lines []string

	for i, original := range writable {
		origUUID, _ := original["uuid"].(string)
		newEntryUUID := uuidMapping[origUUID]

		// Resolve parentUuid, skipping progress ancestors.
		var newParentUUID *string
		parentID, _ := original["parentUuid"].(string)
		for parentID != "" {
			parent, ok := byUUID[parentID]
			if !ok {
				break
			}
			if parentType, _ := parent["type"].(string); parentType != "progress" {
				if mapped, ok := uuidMapping[parentID]; ok {
					newParentUUID = &mapped
				}
				break
			}
			parentID, _ = parent["parentUuid"].(string)
		}

		// Only update timestamp on the last message (leaf detection on resume).
		timestamp := now
		if i != len(writable)-1 {
			if ts, ok := original["timestamp"].(string); ok {
				timestamp = ts
			}
		}

		// Remap logicalParentUuid (compact-boundary backpointer).
		// If the referenced UUID was sliced off (not in uuidMapping),
		// set to nil to avoid leaving a stale reference.
		var newLogicalParent any
		if logicalParent, ok := original["logicalParentUuid"].(string); ok && logicalParent != "" {
			if mapped, ok := uuidMapping[logicalParent]; ok {
				newLogicalParent = mapped
			}
		}

		forked := make(map[string]any, len(original)+6)
		for k, v := range original {
			forked[k] = v
		}
		forked["uuid"] = newEntryUUID
		if newParentUUID != nil {
			forked["parentUuid"] = *newParentUUID
		} else {
			forked["parentUuid"] = nil
		}
		if _, hasLogicalParent := original["logicalParentUuid"]; hasLogicalParent {
			forked["logicalParentUuid"] = newLogicalParent
		}
		forked["sessionId"] = forkedSessionID
		forked["timestamp"] = timestamp
		forked["isSidechain"] = false
		forked["forkedFrom"] = map[string]any{
			"sessionId":   sessionID,
			"messageUuid": origUUID,
		}

		// Remove fields that would leak state from the source session.
		for _, key := range []string{"teamName", "agentName", "slug", "sourceToolAssistantUUID"} {
			delete(forked, key)
		}

		b, err := json.Marshal(forked)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal forked entry: %w", err)
		}
		lines = append(lines, string(b))
	}

	// Append content-replacement entry (if any) with the fork's sessionId.
	if len(contentReplacements) > 0 {
		crEntry := map[string]any{
			"type":         "content-replacement",
			"sessionId":    forkedSessionID,
			"replacements": contentReplacements,
		}
		b, err := json.Marshal(crEntry)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal content-replacement: %w", err)
		}
		lines = append(lines, string(b))
	}

	// Derive title: explicit > original customTitle/aiTitle > first prompt.
	forkTitle := strings.TrimSpace(o.forkTitle)
	if forkTitle == "" {
		contentStr := string(content)
		bufLen := len(contentStr)
		headEnd := bufLen
		if headEnd > liteReadBufSize {
			headEnd = liteReadBufSize
		}
		head := contentStr[:headEnd]

		tailStart := bufLen - liteReadBufSize
		if tailStart < 0 {
			tailStart = 0
		}
		tail := contentStr[tailStart:]

		base := ""
		if v, ok := extractLastJSONStringField(tail, "customTitle"); ok && v != "" {
			base = v
		} else if v, ok := extractLastJSONStringField(head, "customTitle"); ok && v != "" {
			base = v
		} else if v, ok := extractLastJSONStringField(tail, "aiTitle"); ok && v != "" {
			base = v
		} else if v, ok := extractLastJSONStringField(head, "aiTitle"); ok && v != "" {
			base = v
		} else if fp := extractFirstPromptFromHead(head); fp != "" {
			base = fp
		} else {
			base = "Forked session"
		}
		forkTitle = base + " (fork)"
	}

	titleEntry := map[string]any{
		"type":        "custom-title",
		"sessionId":   forkedSessionID,
		"customTitle": forkTitle,
	}
	b, err := json.Marshal(titleEntry)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal title entry: %w", err)
	}
	lines = append(lines, string(b))

	// Write the new session file atomically using O_EXCL to prevent
	// overwriting an existing file.
	forkPath := filepath.Join(projectDir, forkedSessionID+".jsonl")
	fd, err := syscall.Open(forkPath, syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL, 0o600)
	if err != nil {
		return nil, fmt.Errorf("failed to create fork file: %w", err)
	}
	defer syscall.Close(fd)

	data := []byte(strings.Join(lines, "\n") + "\n")
	if _, err := syscall.Write(fd, data); err != nil {
		return nil, fmt.Errorf("failed to write fork file: %w", err)
	}

	return &ForkSessionResult{SessionID: forkedSessionID}, nil
}

// Transcript entry types that carry uuid + parentUuid chain links in fork parsing.
var forkTranscriptTypes = map[string]bool{
	"user": true, "assistant": true, "progress": true,
	"system": true, "attachment": true,
}

// parseForkTranscript parses JSONL content into transcript entries and
// content-replacement records. Only keeps entries that have a uuid and are
// transcript message types. Content-replacement entries are collected for
// re-emission in the fork.
func parseForkTranscript(content []byte, sessionID string) ([]map[string]any, []any) {
	var transcript []map[string]any
	var contentReplacements []any

	start := 0
	length := len(content)
	for start < length {
		end := bytes.IndexByte(content[start:], '\n')
		var lineBytes []byte
		if end == -1 {
			lineBytes = bytes.TrimSpace(content[start:])
			start = length
		} else {
			lineBytes = bytes.TrimSpace(content[start : start+end])
			start = start + end + 1
		}
		if len(lineBytes) == 0 {
			continue
		}

		var entry map[string]any
		if err := json.Unmarshal(lineBytes, &entry); err != nil {
			continue
		}

		entryType, _ := entry["type"].(string)
		uid, hasUUID := entry["uuid"].(string)

		if forkTranscriptTypes[entryType] && hasUUID && uid != "" {
			transcript = append(transcript, entry)
		} else if entryType == "content-replacement" {
			entrySID, _ := entry["sessionId"].(string)
			if entrySID == sessionID {
				if replacements, ok := entry["replacements"].([]any); ok {
					contentReplacements = append(contentReplacements, replacements...)
				}
			}
		}
	}

	return transcript, contentReplacements
}

// newUUID generates a new random UUID v4 string.
func newUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("failed to generate UUID: " + err.Error())
	}
	// Set version 4 and variant bits per RFC 4122.
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// sanitizeUnicode removes dangerous Unicode characters from a string.
//
// Iteratively applies NFKC normalization and strips format (Cf), private
// use (Co), and unassigned (Cn) category characters, plus explicit ranges
// for zero-width chars, directional marks, BOM, and private use area.
// Repeats until stable (max 10 iterations), since NFKC normalization can
// reveal new characters that need stripping.
func sanitizeUnicode(value string) string {
	current := value
	for range 10 {
		previous := current
		// Apply NFKC normalization to handle composed character sequences.
		current = norm.NFKC.String(current)
		// Strip Cf (format), Co (private use), Cn (unassigned) categories
		// and explicit dangerous ranges.
		current = stripInvisible(current)
		if current == previous {
			break
		}
	}
	return current
}

// stripInvisible removes Unicode characters that are invisible or dangerous:
// - General categories Cf (format), Co (private use), Cn (unassigned)
// - Explicit ranges: zero-width chars, directional marks, BOM, private use area
func stripInvisible(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if shouldStripRune(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// shouldStripRune returns true if the rune should be removed during
// Unicode sanitization.
func shouldStripRune(r rune) bool {
	// Explicit ranges matching the upstream TS/Python implementation.
	switch {
	case r >= 0x200B && r <= 0x200F:
		// Zero-width spaces, LTR/RTL marks
		return true
	case r >= 0x202A && r <= 0x202E:
		// Directional formatting characters
		return true
	case r >= 0x2066 && r <= 0x2069:
		// Directional isolates
		return true
	case r == 0xFEFF:
		// Byte order mark
		return true
	case r >= 0xE000 && r <= 0xF8FF:
		// Basic Multilingual Plane private use
		return true
	case r >= 0xF0000 && r <= 0xFFFFD:
		// Supplementary private use area A
		return true
	case r >= 0x100000 && r <= 0x10FFFD:
		// Supplementary private use area B
		return true
	}

	// General category checks: Cf (format), Co (private use), Cn (unassigned).
	// These catch characters not covered by the explicit ranges above.
	if unicode.Is(unicode.Cf, r) || unicode.Is(unicode.Co, r) {
		return true
	}
	// Cn (unassigned) -- not in any defined Unicode category.
	// In Go, a rune that is not in any category table is effectively
	// unassigned. We check by verifying it is not a letter, number,
	// mark, punctuation, symbol, separator, or the explicit categories
	// above.
	if !unicode.IsGraphic(r) && !unicode.IsSpace(r) && !unicode.IsControl(r) {
		return true
	}

	return false
}

// compactJSONString produces a JSON-encoded string value (with quotes).
// Uses Go's json.Marshal semantics for proper escaping.
func compactJSONString(s string) string {
	// For most titles this is just wrapping in quotes, but we need proper
	// JSON escaping for special characters.
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				b.WriteString(fmt.Sprintf(`\u%04x`, r))
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// appendToSession searches for an existing session file and appends data to it.
//
// Uses O_WRONLY | O_APPEND (no O_CREAT) so the open fails with ENOENT for
// missing files, avoiding TOCTOU races during the candidate search.
func appendToSession(sessionID, data string, o *sessionOptions) error {
	fileName := sessionID + ".jsonl"
	configDir := getClaudeConfigDir(o.configDir)

	if o.directory != "" {
		canonical := canonicalizePath(o.directory)

		// Try the exact/prefix-matched project directory first.
		projDir := findProjectDir(configDir, canonical)
		if projDir != "" {
			ok, err := tryAppend(filepath.Join(projDir, fileName), data)
			if err != nil {
				return err
			}
			if ok {
				return nil
			}
		}

		// Worktree fallback -- matches ListSessions/GetSessionMessages.
		worktreePaths := getWorktreePaths(canonical)
		for _, wt := range worktreePaths {
			if wt == canonical {
				continue // already tried above
			}
			wtProjDir := findProjectDir(configDir, wt)
			if wtProjDir != "" {
				ok, err := tryAppend(filepath.Join(wtProjDir, fileName), data)
				if err != nil {
					return err
				}
				if ok {
					return nil
				}
			}
		}

		return fmt.Errorf("session %s not found in project directory for %s", sessionID, o.directory)
	}

	// No directory -- search all project directories.
	projectsDir := getProjectsDir(configDir)
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return fmt.Errorf("session %s not found (no projects directory)", sessionID)
	}
	for _, entry := range entries {
		ok, appendErr := tryAppend(filepath.Join(projectsDir, entry.Name(), fileName), data)
		if appendErr != nil {
			return appendErr
		}
		if ok {
			return nil
		}
	}
	return fmt.Errorf("session %s not found in any project directory", sessionID)
}

// tryAppend attempts to append data to a file at path.
//
// Opens with O_WRONLY | O_APPEND (no O_CREAT), so the open fails with
// ENOENT if the file does not exist. Returns (true, nil) on successful
// write, (false, nil) if the file does not exist or is 0-byte, and
// (false, err) for real write failures (ENOSPC, EACCES, EIO, etc.).
//
// A 0-byte .jsonl is a stub that readers already skip; without this guard
// the search would stop at an empty stub while the real file lives elsewhere.
func tryAppend(path, data string) (bool, error) {
	fd, err := syscall.Open(path, syscall.O_WRONLY|syscall.O_APPEND, 0)
	if err != nil {
		if errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ENOTDIR) {
			return false, nil
		}
		return false, err
	}
	defer syscall.Close(fd)

	var stat syscall.Stat_t
	if err := syscall.Fstat(fd, &stat); err != nil {
		return false, err
	}
	if stat.Size == 0 {
		return false, nil
	}

	_, err = syscall.Write(fd, []byte(data))
	if err != nil {
		return false, err
	}
	return true, nil
}
