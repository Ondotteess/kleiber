package version

import "testing"

func TestCurrent_NonEmpty(t *testing.T) {
	if got := Current(); got == "" {
		t.Fatal("Current() returned an empty string; expected at least the dev fallback")
	}
}
