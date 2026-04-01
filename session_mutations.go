package claudecode

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
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
