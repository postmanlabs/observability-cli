package learn

import (
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
