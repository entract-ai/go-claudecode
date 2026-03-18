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
}
