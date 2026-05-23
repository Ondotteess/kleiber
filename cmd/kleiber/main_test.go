package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRun_Version_PrintsVersion(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"long flag", []string{"--version"}},
		{"short flag", []string{"-v"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if err := run(tc.args, &stdout, &stderr); err != nil {
				t.Fatalf("run returned error: %v", err)
			}
			got := stdout.String()
			if !strings.HasPrefix(got, "kleiber ") {
				t.Errorf("stdout = %q, want prefix %q", got, "kleiber ")
			}
			if stderr.Len() != 0 {
				t.Errorf("stderr = %q, want empty", stderr.String())
			}
		})
	}
}

func TestRun_NoArgs_PrintsPreAlphaNotice(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := run(nil, &stdout, &stderr); err != nil {
		t.Fatalf("run returned error: %v", err)
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "pre-alpha") {
		t.Errorf("stderr = %q, want it to mention pre-alpha", stderr.String())
	}
}
