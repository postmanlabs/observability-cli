package learn

import (
	"io"
)

type stripControlCharactersReader struct {
	wrapped io.Reader
}

func newStripControlCharactersReader(wrapped io.Reader) stripControlCharactersReader {
	return stripControlCharactersReader{wrapped: wrapped}
}

// Read up to len(p) bytes, removing any control characters found.
// Removed characters do not count toward the total bytes read.  Returns the
// number of bytes read.
func (r stripControlCharactersReader) Read(p []byte) (n int, err error) {
	pIdx := 0
	buf := make([]byte, len(p))

	// Read up to len(p) bytes, then discard any control characters.  Continue
	// reading (and discarding control characters) until p is full or there are
	// no more bytes to read.
	for pIdx < len(p) {
		remaining := len(p) - pIdx
		bufSlice := buf[:remaining]

		var bufN int
		bufN, err = r.wrapped.Read(bufSlice)

		// Copy from buf to p, skipping control characters.
		for _, c := range bufSlice {
			if c < 0x1f {
				continue
			}
			p[pIdx] = c
			pIdx += 1
		}

		// If we hit an error or read fewer bytes than the size of the buffer,
		// don't bother trying to read more from the underlying reader.
		if err != nil || bufN < remaining {
			break
		}

		// Otherwise, we loop to replace CRs we dropped.
	}

	return pIdx, err
}
