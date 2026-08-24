package appserver

import "testing"

func TestTakeoverValidation(t *testing.T) {
	const threadID = "01a03232-664d-7223-8fe8-595cbb1ed353"
	if !validThreadID(threadID) {
		t.Fatal("valid thread id rejected")
	}
	for _, invalid := range []string{
		"../" + threadID,
		"01a03232664d-7223-8fe8-595cbb1ed353",
		"01a03232-664d-7223-8fe8-595cbb1ed35z",
	} {
		if validThreadID(invalid) {
			t.Fatalf("invalid thread id accepted: %q", invalid)
		}
	}
	if got := appBundle("/Applications/ChatGPT.app/Contents/Resources/codex"); got != "/Applications/ChatGPT.app" {
		t.Fatalf("app bundle = %q", got)
	}
	if got := appBundle("/usr/local/bin/codex"); got != "" {
		t.Fatalf("CLI app bundle = %q", got)
	}
}
