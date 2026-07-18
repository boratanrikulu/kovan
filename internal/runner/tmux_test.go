package runner

import (
	"reflect"
	"strings"
	"testing"
)

func TestScrollBindings(t *testing.T) {
	joined := make([]string, len(scrollBindings))
	for i, b := range scrollBindings {
		if b[0] != "bind-key" {
			t.Errorf("binding %d is not a bind-key: %v", i, b)
		}
		joined[i] = strings.Join(b, " ")
	}
	all := strings.Join(joined, "\n")
	// Wheel-up enters copy-mode for non-mouse apps instead of an arrow-key burst.
	if !strings.Contains(all, "WheelUpPane if-shell -F -t = #{mouse_any_flag} send-keys -M copy-mode -e") {
		t.Errorf("missing the root WheelUpPane copy-mode binding:\n%s", all)
	}
	// Each notch scrolls a gentle 2 lines, in both copy-mode key tables.
	for _, want := range []string{
		"copy-mode WheelUpPane send-keys -X -N 2 scroll-up",
		"copy-mode WheelDownPane send-keys -X -N 2 scroll-down",
		"copy-mode-vi WheelUpPane send-keys -X -N 2 scroll-up",
		"copy-mode-vi WheelDownPane send-keys -X -N 2 scroll-down",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("missing binding %q:\n%s", want, all)
		}
	}
}

func TestTailLines(t *testing.T) {
	cases := []struct {
		name string
		in   string
		n    int
		want string
	}{
		{"last two", "a\nb\nc\nd\n", 2, "c\nd"},
		{"trailing blanks trimmed", "a\nb\n\n\n", 10, "a\nb"},
		{"fewer than n", "a\nb", 5, "a\nb"},
		{"n zero keeps all", "a\nb\nc", 0, "a\nb\nc"},
		{"empty", "", 5, ""},
		{"only blanks", "\n\n\n", 5, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := tailLines(c.in, c.n); got != c.want {
				t.Errorf("tailLines(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
			}
		})
	}
}

func TestSplitOption(t *testing.T) {
	cases := []struct {
		in, key, value string
	}{
		{"mouse on", "mouse", "on"},
		{"history-limit 50000", "history-limit", "50000"},
		{"  mouse   on  ", "mouse", "on"},
		{"status off", "status", "off"},
		{"focus-events", "focus-events", ""},
		{"", "", ""},
	}
	for _, c := range cases {
		key, value := splitOption(c.in)
		if key != c.key || value != c.value {
			t.Errorf("splitOption(%q) = (%q, %q), want (%q, %q)", c.in, key, value, c.key, c.value)
		}
	}
}

func TestAttachPlan(t *testing.T) {
	args, blocking := attachPlan("S", false)
	if !blocking || !reflect.DeepEqual(args, []string{"attach", "-t", "S"}) {
		t.Errorf("outside tmux: got args=%v blocking=%v, want attach blocking", args, blocking)
	}

	args, blocking = attachPlan("S", true)
	if blocking || !reflect.DeepEqual(args, []string{"switch-client", "-t", "S"}) {
		t.Errorf("inside tmux: got args=%v blocking=%v, want switch-client non-blocking", args, blocking)
	}
}
