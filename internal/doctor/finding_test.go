package doctor

import "testing"

func TestSeverity_String(t *testing.T) {
	cases := []struct {
		sev  Severity
		want string
	}{
		{SeverityOK, "ok"},
		{SeverityInfo, "info"},
		{SeverityWarning, "warning"},
		{SeverityError, "error"},
		{Severity(99), "unknown"},
	}
	for _, tc := range cases {
		if got := tc.sev.String(); got != tc.want {
			t.Errorf("Severity(%d).String() = %q, want %q", int(tc.sev), got, tc.want)
		}
	}
}

func TestSeverity_Order(t *testing.T) {
	// The numeric ordering matters for HighestSeverity and for callers
	// that compare severities. Encode the expected progression so a
	// future renumber breaks this test loudly.
	order := []Severity{SeverityOK, SeverityInfo, SeverityWarning, SeverityError}
	for i := 1; i < len(order); i++ {
		if !(order[i-1] < order[i]) {
			t.Errorf("Severity ordering broken at index %d: %v not < %v", i, order[i-1], order[i])
		}
	}
}

func TestHighestSeverity_Empty(t *testing.T) {
	if got := HighestSeverity(nil); got != SeverityOK {
		t.Errorf("HighestSeverity(nil) = %v, want SeverityOK", got)
	}
}

func TestHighestSeverity_Multiple(t *testing.T) {
	fs := []Finding{
		{Severity: SeverityOK},
		{Severity: SeverityWarning},
		{Severity: SeverityInfo},
		{Severity: SeverityError},
		{Severity: SeverityWarning},
	}
	if got := HighestSeverity(fs); got != SeverityError {
		t.Errorf("HighestSeverity = %v, want SeverityError", got)
	}
}
