package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestExitCodes(t *testing.T) {
	for _, tc := range []struct {
		args []string
		code int
		out  string
	}{
		{nil, 0, "Usage: mmp"},
		{[]string{"version"}, 0, "mmp dev"},
		{[]string{"--version"}, 0, "mmp dev"},
		{[]string{"version", "x"}, 2, ""},
		{[]string{"nothing"}, 2, ""},
		{[]string{"lab"}, 2, ""},
		{[]string{"agent"}, 2, ""},
		{[]string{"agent", "frob"}, 2, ""},
		{[]string{"claim"}, 2, ""},
		{[]string{"queue", "-role", "nobody"}, 2, ""},
		{[]string{"trust", "-frob"}, 2, ""},
		{[]string{"fidelity"}, 2, ""},
		{[]string{"paper"}, 2, ""},
		{[]string{"site"}, 2, ""},
		{[]string{"status", "-only", "done"}, 2, ""},
		{[]string{"check", "-content", "/does/not/exist"}, 1, ""},
	} {
		var out, errb bytes.Buffer
		if got := run(tc.args, &out, &errb); got != tc.code {
			t.Errorf("mmp %v: exit %d, want %d (%s)", tc.args, got, tc.code, errb.String())
		}
		if !strings.Contains(out.String(), tc.out) {
			t.Errorf("mmp %v: the output does not have %q", tc.args, tc.out)
		}
	}
}

func TestNamesAreUnique(t *testing.T) {
	n := names()
	for i := 1; i < len(n); i++ {
		if n[i] == n[i-1] {
			t.Errorf("two commands have the name %s", n[i])
		}
	}
}
