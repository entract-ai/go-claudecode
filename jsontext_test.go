package claudecode

import (
	"github.com/go-json-experiment/json/jsontext"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestJsontextStreamingBehavior verifies the behavior of jsontext.Decoder for streaming JSON.
// These tests ensure our assumptions about jsontext behavior are correct.
func TestJsontextStreamingBehavior(t *testing.T) {
	t.Run("multiple JSON values in stream", func(t *testing.T) {
		input := `{"a":1}{"b":2}{"c":3}`
		dec := jsontext.NewDecoder(strings.NewReader(input))

		var values []string
		for {
			val, err := dec.ReadValue()
			if err == io.EOF {
				break
			}
			require.NoError(t, err)
			values = append(values, string(val))
		}

		assert.Equal(t, []string{`{"a":1}`, `{"b":2}`, `{"c":3}`}, values)
	})

	t.Run("large value over 2MB", func(t *testing.T) {
		// Create a JSON value larger than 2MB to verify no buffer limit
		largeContent := strings.Repeat("x", 2*1024*1024+1000)
		input := `{"content":"` + largeContent + `"}`

		dec := jsontext.NewDecoder(strings.NewReader(input))
		val, err := dec.ReadValue()
		require.NoError(t, err)
		assert.True(t, len(val) > 2*1024*1024)
	})

	t.Run("empty input returns EOF", func(t *testing.T) {
		dec := jsontext.NewDecoder(strings.NewReader(""))
		_, err := dec.ReadValue()
		assert.ErrorIs(t, err, io.EOF)
	})

	t.Run("only whitespace returns EOF", func(t *testing.T) {
		dec := jsontext.NewDecoder(strings.NewReader("   \n\t\n   "))
		_, err := dec.ReadValue()
		assert.ErrorIs(t, err, io.EOF)
	})

	t.Run("incomplete JSON returns ErrUnexpectedEOF", func(t *testing.T) {
		dec := jsontext.NewDecoder(strings.NewReader(`{"incomplete`))
		_, err := dec.ReadValue()
		require.Error(t, err)
		assert.ErrorIs(t, err, io.ErrUnexpectedEOF)
	})

	t.Run("whitespace between values handled", func(t *testing.T) {
		input := `{"a":1}
  {"b":2}
	{"c":3}`
		dec := jsontext.NewDecoder(strings.NewReader(input))

		var values []string
		for {
			val, err := dec.ReadValue()
			if err == io.EOF {
				break
			}
			require.NoError(t, err)
			values = append(values, string(val))
		}

		assert.Equal(t, []string{`{"a":1}`, `{"b":2}`, `{"c":3}`}, values)
	})

	t.Run("value is stripped of leading whitespace", func(t *testing.T) {
		dec := jsontext.NewDecoder(strings.NewReader("   {\"a\":1}"))
		val, err := dec.ReadValue()
		require.NoError(t, err)
		assert.Equal(t, `{"a":1}`, string(val))
	})

	t.Run("newline-delimited JSON stream", func(t *testing.T) {
		input := "{\"type\":\"message\",\"id\":1}\n{\"type\":\"message\",\"id\":2}\n{\"type\":\"message\",\"id\":3}\n"
		dec := jsontext.NewDecoder(strings.NewReader(input))

		var values []string
		for {
			val, err := dec.ReadValue()
			if err == io.EOF {
				break
			}
			require.NoError(t, err)
			values = append(values, string(val))
		}

		assert.Len(t, values, 3)
	})

	t.Run("raw decoder fails on non-JSON debug lines", func(t *testing.T) {
		// jsontext.Decoder fails when non-JSON text like [SandboxDebug]
		// lines appear in the stream. This is the underlying issue that
		// the jsonLineFilterReader fixes.
		input := "[SandboxDebug] Seccomp filtering not available\n" +
			`{"type":"system","subtype":"init"}` + "\n"

		dec := jsontext.NewDecoder(strings.NewReader(input))

		_, err := dec.ReadValue()
		assert.Error(t, err, "raw jsontext.Decoder should error on non-JSON lines")
	})

	t.Run("filtered reader skips non-JSON debug lines", func(t *testing.T) {
		// jsonLineFilterReader strips non-JSON lines so the decoder
		// only sees valid JSON objects. This is the fix for the
		// [SandboxDebug] corruption bug (upstream Python commit c290bbf).
		input := "[SandboxDebug] Seccomp filtering not available\n" +
			`{"type":"system","subtype":"init"}` + "\n" +
			"[SandboxDebug] another debug line\n" +
			`{"type":"result","subtype":"success"}` + "\n"

		filtered := newJSONLineFilterReader(strings.NewReader(input))
		dec := jsontext.NewDecoder(filtered)

		var values []string
		for {
			val, err := dec.ReadValue()
			if err == io.EOF {
				break
			}
			require.NoError(t, err)
			values = append(values, string(val))
		}

		require.Len(t, values, 2)
		assert.Contains(t, values[0], `"type":"system"`)
		assert.Contains(t, values[1], `"type":"result"`)
	})

	t.Run("filtered reader handles interleaved non-JSON lines", func(t *testing.T) {
		// Debug/warning lines interleaved between valid JSON messages
		// must be silently skipped.
		input := "[SandboxDebug] line 1\n" +
			"[SandboxDebug] line 2\n" +
			`{"type":"system","subtype":"init"}` + "\n" +
			"WARNING: something\n" +
			`{"type":"result","subtype":"success"}` + "\n"

		filtered := newJSONLineFilterReader(strings.NewReader(input))
		dec := jsontext.NewDecoder(filtered)

		var values []string
		for {
			val, err := dec.ReadValue()
			if err == io.EOF {
				break
			}
			require.NoError(t, err)
			values = append(values, string(val))
		}

		require.Len(t, values, 2)
		assert.Contains(t, values[0], `"type":"system"`)
		assert.Contains(t, values[1], `"type":"result"`)
	})

	t.Run("filtered reader passes through all-JSON stream unchanged", func(t *testing.T) {
		// When there are no non-JSON lines, the filter is transparent.
		input := `{"a":1}` + "\n" + `{"b":2}` + "\n" + `{"c":3}` + "\n"

		filtered := newJSONLineFilterReader(strings.NewReader(input))
		dec := jsontext.NewDecoder(filtered)

		var values []string
		for {
			val, err := dec.ReadValue()
			if err == io.EOF {
				break
			}
			require.NoError(t, err)
			values = append(values, string(val))
		}

		assert.Len(t, values, 3)
	})

	t.Run("filtered reader handles empty lines", func(t *testing.T) {
		input := "\n\n" + `{"a":1}` + "\n" + "\n" + `{"b":2}` + "\n"

		filtered := newJSONLineFilterReader(strings.NewReader(input))
		dec := jsontext.NewDecoder(filtered)

		var values []string
		for {
			val, err := dec.ReadValue()
			if err == io.EOF {
				break
			}
			require.NoError(t, err)
			values = append(values, string(val))
		}

		assert.Len(t, values, 2)
	})

	t.Run("filtered reader handles only non-JSON lines", func(t *testing.T) {
		input := "[SandboxDebug] line 1\n[SandboxDebug] line 2\nWARNING: stuff\n"

		filtered := newJSONLineFilterReader(strings.NewReader(input))
		dec := jsontext.NewDecoder(filtered)

		_, err := dec.ReadValue()
		assert.ErrorIs(t, err, io.EOF)
	})

	t.Run("filtered reader handles JSON lines exceeding 64KB", func(t *testing.T) {
		// The default bufio.Scanner buffer is 64KB. Assistant responses
		// routinely exceed this, so the scanner must be configured with
		// a larger buffer. Without the Buffer() call in
		// newJSONLineFilterReader, this test fails with bufio.ErrTooLong.
		largeContent := strings.Repeat("x", 128*1024) // 128KB > default 64KB limit
		input := `{"type":"system","subtype":"init"}` + "\n" +
			`{"type":"assistant","content":"` + largeContent + `"}` + "\n" +
			`{"type":"result","subtype":"success"}` + "\n"

		filtered := newJSONLineFilterReader(strings.NewReader(input))
		dec := jsontext.NewDecoder(filtered)

		var values []string
		for {
			val, err := dec.ReadValue()
			if err == io.EOF {
				break
			}
			require.NoError(t, err)
			values = append(values, string(val))
		}

		require.Len(t, values, 3)
		assert.Contains(t, values[0], `"type":"system"`)
		assert.Contains(t, values[1], `"type":"assistant"`)
		assert.True(t, len(values[1]) > 128*1024, "large JSON value should be preserved")
		assert.Contains(t, values[2], `"type":"result"`)
	})
}
