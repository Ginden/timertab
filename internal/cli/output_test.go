package cli

import (
	"strings"
	"testing"
)

func TestHighlightUsesReadableColorsOnDarkTerminals(t *testing.T) {
	got := highlight("ini", "[Unit]\nDescription=timertab job\n# managed unit\n")

	if !strings.Contains(got, "\x1b[") {
		t.Fatalf("highlight() returned no ANSI styling: %q", got)
	}
	if strings.Contains(got, "\x1b[38;5;0m") {
		t.Fatalf("highlight() used a black foreground on terminal output: %q", got)
	}
}
