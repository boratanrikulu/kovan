package agent

import (
	"strings"
	"unicode/utf8"
)

// DigestMaxEntries caps how many conversation entries a digest carries.
const DigestMaxEntries = 40

// Clip widths for digest entries, in runes.
const (
	digestTextClip = 300
	digestToolClip = 120
)

// digest collects the labeled lines every driver's Digest renders.
type digest struct{ entries []string }

func (d *digest) you(text string) {
	if s := oneLine(text); s != "" {
		d.entries = append(d.entries, "you: "+clip(s, digestTextClip))
	}
}

func (d *digest) agent(text string) {
	if s := oneLine(text); s != "" {
		d.entries = append(d.entries, "agent: "+clip(s, digestTextClip))
	}
}

// tool renders a tool call as one line: the tool name plus the most telling
// scrap of its input.
func (d *digest) tool(name string, input map[string]any) {
	e := "agent ran " + name
	for _, key := range []string{"description", "command", "cmd", "file_path", "prompt"} {
		if v, ok := input[key].(string); ok && v != "" {
			e += ": " + clip(oneLine(v), digestToolClip)
			break
		}
	}
	d.entries = append(d.entries, e)
}

func (d *digest) String() string {
	e := d.entries
	if len(e) > DigestMaxEntries {
		e = e[len(e)-DigestMaxEntries:]
	}
	return strings.Join(e, "\n")
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func clip(s string, width int) string {
	if width < 1 {
		return ""
	}
	if utf8.RuneCountInString(s) <= width {
		return s
	}
	return string([]rune(s)[:width-1]) + "…"
}
