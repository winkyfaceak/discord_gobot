package navidrome

import (
	"bufio"
	"bytes"
	"io"
	"testing"
)

// oggPage builds a page whose segment table is given explicitly, so tests
// can split packets across pages.
func oggPage(segments []byte, data []byte) []byte {
	page := append([]byte("OggS"), make([]byte, 22)...)
	page = append(page, byte(len(segments)))
	page = append(page, segments...)
	return append(page, data...)
}

func TestOpusStreamReassemblesPacketsAndSkipsHeaders(t *testing.T) {
	long := bytes.Repeat([]byte{'L'}, 300)  // 255 + 45
	exact := bytes.Repeat([]byte{'E'}, 255) // 255 + 0 terminator
	split := bytes.Repeat([]byte{'S'}, 260) // 255 on page 2, 5 on page 3

	var stream []byte
	stream = append(stream, oggPage([]byte{8, 4}, []byte("OpusHeadTags"))...)
	stream = append(stream, oggPage([]byte{255, 45, 255, 0, 3, 255}, bytes.Join([][]byte{long, exact, []byte("abc"), split[:255]}, nil))...)
	stream = append(stream, oggPage([]byte{5}, split[255:])...)

	o := &OpusStream{body: io.NopCloser(nil), r: bufio.NewReader(bytes.NewReader(stream))}
	for _, want := range [][]byte{long, exact, []byte("abc"), split} {
		got, err := o.Next()
		if err != nil {
			t.Fatalf("Next() error = %v, want packet of %d bytes", err, len(want))
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("Next() = %d bytes %q..., want %d bytes %q...", len(got), got[:1], len(want), want[:1])
		}
	}
	if _, err := o.Next(); err != io.EOF {
		t.Fatalf("Next() at end error = %v, want io.EOF", err)
	}
}
