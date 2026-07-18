package app

import (
	"encoding/hex"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// captureClipboardImage writes the clipboard's image to out as PNG (macOS only).
// It prefers `pngpaste`, falling back to osascript. ok is false — with no error —
// when the clipboard holds no image (or the platform isn't supported), so the
// caller can show a clean message instead of failing. err is non-nil only on an
// unexpected failure. It is standalone so a future `kovan attach` can reuse it.
func captureClipboardImage(out string) (ok bool, err error) {
	if runtime.GOOS != "darwin" {
		return false, nil
	}
	if _, err := exec.LookPath("pngpaste"); err == nil {
		// pngpaste exits non-zero when the clipboard has no image.
		if err := exec.Command("pngpaste", out).Run(); err != nil {
			return false, nil
		}
		return fileNonEmpty(out), nil
	}
	// Fallback: osascript returns «data PNGf<hex>», or errors when there's no image.
	raw, err := exec.Command("osascript", "-e", "the clipboard as «class PNGf»").Output()
	if err != nil {
		return false, nil
	}
	data, found := decodePNGfHex(string(raw))
	if !found {
		return false, nil
	}
	if err := os.WriteFile(out, data, 0o644); err != nil {
		return false, err
	}
	return true, nil
}

// decodePNGfHex pulls the PNG bytes out of osascript's «data PNGf...» rendering.
// found is false (no error) when the text carries no PNGf payload, so a "no
// image" reply is handled cleanly.
func decodePNGfHex(s string) (data []byte, found bool) {
	const marker = "PNGf"
	i := strings.Index(s, marker)
	if i < 0 {
		return nil, false
	}
	rest := s[i+len(marker):]
	// '»' is a multi-byte rune, so match it as a rune, not a byte.
	if j := strings.IndexRune(rest, '»'); j >= 0 {
		rest = rest[:j]
	}
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return nil, false
	}
	b, err := hex.DecodeString(rest)
	if err != nil || len(b) == 0 {
		return nil, false
	}
	return b, true
}

func fileNonEmpty(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Size() > 0
}
