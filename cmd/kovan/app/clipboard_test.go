package app

import (
	"encoding/hex"
	"testing"
)

func TestDecodePNGfHex(t *testing.T) {
	// A "no image" / unrelated reply yields no payload, without erroring.
	for _, s := range []string{"", "error: can't get the clipboard as «class PNGf»", "«class furl»"} {
		if data, ok := decodePNGfHex(s); ok || data != nil {
			t.Errorf("decodePNGfHex(%q) = (%v,%v), want (nil,false)", s, data, ok)
		}
	}

	// A real «data PNGf<hex>» reply decodes to the raw bytes.
	want := []byte{0x89, 0x50, 0x4e, 0x47}
	out := "«data PNGf" + hex.EncodeToString(want) + "»\n"
	got, ok := decodePNGfHex(out)
	if !ok {
		t.Fatalf("decodePNGfHex(%q) found nothing", out)
	}
	if string(got) != string(want) {
		t.Errorf("decoded % x, want % x", got, want)
	}
}
