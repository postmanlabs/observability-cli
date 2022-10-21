package learn

import (
	"encoding/json"
	"io"
	"strings"
	"testing"
	"unicode"

	"github.com/stretchr/testify/assert"
)

func TestSkipCarriageReturnReader(t *testing.T) {
	testCases := []struct {
		Name                 string
		Input                string
		BytesToRead          int
		DifferentEOFBehavior bool
	}{
		{
			Name:        "empty",
			Input:       "",
			BytesToRead: 128,
		},
		{
			Name:        "read 0 bytes",
			Input:       "foo",
			BytesToRead: 0,
		},
		{
			Name:        "no CR string",
			Input:       "foo",
			BytesToRead: 128,
		},
		{
			Name:        "CR",
			Input:       "foo\rbar",
			BytesToRead: 128,
		},
		{
			Name:        "CRs",
			Input:       "foo\rbar\r",
			BytesToRead: 128,
		},
		{
			Name:        "fill in after removing CRs",
			Input:       "foo\rbar",
			BytesToRead: 4,
		},
		{
			Name:        "CRLFs",
			Input:       "f\r\noo\r\nbar\r\n",
			BytesToRead: 128,
		},
		{
			Name:        "fill in after removing CRLFs",
			Input:       "f\r\noo\r\nbar\r\n",
			BytesToRead: 4,
		},
		{
			Name:        "fill in after removing trailing CRLFs",
			Input:       "f\r\noo\r\nbar\r\n",
			BytesToRead: 7,

			// The strip reader returns an EOF where the strings.Reader
			// doesn't, because it tries to read more characters after the
			// trailing CRLF and fails.
			DifferentEOFBehavior: true,
		},
	}

	// For each test case, read bytes from the CR stripper and compare the
	// results to reading bytes straight from a strings.Reader and then
	// manually removing CRs.
	for _, tc := range testCases {
		numCRs := countControlCharacters(tc.Input)

		// Read bytes from the CR stripper.
		buf := make([]byte, tc.BytesToRead)
		n, err := newStripControlCharactersReader(strings.NewReader(tc.Input)).Read(buf)

		// Read bytes straight from strings.Reader. Read extra bytes to make up
		// for manually removing CRs afterwards.
		expectedBuf := make([]byte, tc.BytesToRead+numCRs)
		expectedN, expectedErr := strings.NewReader(tc.Input).Read(expectedBuf)

		// Manually remove CRs from the string read from the strings.Reader.
		expectedStr := removeControlCharacters(string(expectedBuf))

		// Copy the expected string into a BytesToRead-sized buffer, to
		// ensure the sizes of the underlying buffers match.
		expectedBuf = make([]byte, tc.BytesToRead)
		copy(expectedBuf, expectedStr)

		if !(tc.DifferentEOFBehavior && err == io.EOF) {
			assert.Equal(t, expectedErr, err, tc.Name+": error")
		}
		assert.Equal(t, expectedBuf, buf, tc.Name+": string")
		assert.Equal(t, expectedN-numCRs, n, tc.Name+": character count")
	}
}

func TestSkipControlCharacterReader_JSON(t *testing.T) {
	testCases := []struct {
		Name     string
		Input    string
		Expected interface{}
	}{
		{
			Name:     "no control characters",
			Input:    `{"foo": "bar"}`,
			Expected: map[string]interface{}{"foo": "bar"},
		},
		{
			Name:     "remove control characters",
			Input:    "{\"foo\r\n\": \"bar\"}",
			Expected: map[string]interface{}{"foo": "bar"},
		},
		{
			Name:  "escaped control char",
			Input: "{\"foo\\\r\": \"bar\"}",

			// The Go JSON parser doesn't support escaping control characters.
			// However, if someone were to try, the preprocessor would remove
			// the control character and replace it with a backslash.
			Expected: map[string]interface{}{`foo\`: "bar"},
		},
		{
			Name: "newline in string",
			Input: `{
  "greeting": "hello \
",
  "subject": "world"
}`,
			Expected: map[string]interface{}{"greeting": "hello \\", "subject": "world"},
		},
	}

	for _, tc := range testCases {
		// Parse JSON after removing control strings.
		var parsed interface{}
		var err error

		// Try parsing using a JSON decoder.
		decoder := json.NewDecoder(newStripControlCharactersReader(strings.NewReader(tc.Input)))
		err = decoder.Decode(&parsed)
		assert.NoError(t, err, "["+tc.Name+", decoder] error")
		assert.Equal(t, tc.Expected, parsed, "["+tc.Name+", decoder] not equal")

		// Try reading all bytes from the reader, one byte at a time, and then
		// parsing.
		bytesToRead := 1
		reader := newStripControlCharactersReader(strings.NewReader(tc.Input))
		read := make([]byte, 0, len([]byte(tc.Input)))
		buf := make([]byte, bytesToRead)
		for {
			var n int
			n, err = reader.Read(buf)

			read = append(read, buf[:n]...)

			if err != nil {
				break
			}

			// Clear the buffer.
			for i, _ := range buf {
				buf[i] = '\x00'
			}
		}

		// Parse.
		if err == io.EOF {
			err = json.Unmarshal(read, &parsed)
		}
		assert.NoError(t, err, "["+tc.Name+", byte-by-byte] error")
		assert.Equal(t, tc.Expected, parsed, "["+tc.Name+", byte-by-byte] not equal")
	}
}

func countControlCharacters(s string) (count int) {
	for _, r := range s {
		if unicode.IsControl(r) {
			count += 1
		}
	}

	return count
}

func removeControlCharacters(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}
