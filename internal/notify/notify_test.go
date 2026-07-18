package notify

import "testing"

func TestFor(t *testing.T) {
	if _, ok := For("macos").(MacOS); !ok {
		t.Error("macos should map to MacOS")
	}
	if _, ok := For("none").(Noop); !ok {
		t.Error("none should map to Noop")
	}
	if _, ok := For("").(Noop); !ok {
		t.Error("empty should map to Noop")
	}
}

func TestQuote(t *testing.T) {
	if got := quote(`a"b\c`); got != `"a\"b\\c"` {
		t.Errorf("quote = %s", got)
	}
}
