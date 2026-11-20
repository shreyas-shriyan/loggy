package clipboard

import (
	"bytes"
	"testing"
)

func TestEncodeUTF16LE(t *testing.T) {
	got := encodeUTF16LE("aé\n")
	want := []byte{0xFF, 0xFE, 'a', 0, 0xE9, 0, '\r', 0, '\n', 0}
	if !bytes.Equal(got, want) {
		t.Fatalf("got % x, want % x", got, want)
	}
}
