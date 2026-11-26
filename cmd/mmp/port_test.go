package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const goodCommit = "9097e2f90e66d2f6244209cb1c07f7120d17ec53"

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func portTree(t *testing.T, manifest string, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "manifests", "ported.yaml"), manifest)
	for name, text := range files {
		writeFile(t, filepath.Join(root, name), text)
	}
	return root
}

const goodManifest = `- source: src
  repo: https://github.com/someone/src
  commit: ` + goodCommit + `
  licence: Apache-2.0
  date: 2026-11-25
  paths: [Src/A.lean]
  into: MinimalModel/Ported/Src
`

const goodFile = "/-\nCopyright (c) 2026 Someone. All rights reserved.\n-/\n-- Ported from someone/src at commit " + goodCommit + ", file `Src/A.lean`.\nmodule\n"

func TestPortCheckGood(t *testing.T) {
	root := portTree(t, goodManifest, map[string]string{"MinimalModel/Ported/Src/A.lean": goodFile})
	probs, n, err := portCheck(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(probs) != 0 || n != 1 {
		t.Fatalf("got %d files and the problems %v", n, probs)
	}
}

func TestPortCheckProblems(t *testing.T) {
	for _, tc := range []struct {
		name     string
		manifest string
		files    map[string]string
		want     string
	}{
		{"no provenance", goodManifest, map[string]string{"MinimalModel/Ported/Src/A.lean": "/-\nCopyright (c) 2026 Someone.\n-/\nmodule\n"}, "no provenance line"},
		{"wrong commit", goodManifest, map[string]string{"MinimalModel/Ported/Src/A.lean": strings.Replace(goodFile, "9097", "1111", 1)}, "no provenance line"},
		{"no header", goodManifest, map[string]string{"MinimalModel/Ported/Src/A.lean": "-- Ported from someone/src at commit " + goodCommit + "\n"}, "licence header"},
		{"orphan", goodManifest, map[string]string{"MinimalModel/Ported/Src/A.lean": goodFile, "MinimalModel/Ported/Other/B.lean": goodFile}, "in no entry"},
		{"bad licence", strings.Replace(goodManifest, "Apache-2.0", "GPL-3.0", 1), map[string]string{"MinimalModel/Ported/Src/A.lean": goodFile}, "licence GPL-3.0"},
		{"short commit", strings.Replace(goodManifest, goodCommit, "9097e2f", 1), map[string]string{"MinimalModel/Ported/Src/A.lean": goodFile}, "40 hex digits"},
		{"target outside", strings.Replace(goodManifest, "MinimalModel/Ported/Src", "MinimalModel/Src", 1), map[string]string{"MinimalModel/Ported/Src/A.lean": goodFile}, "not in MinimalModel/Ported"},
	} {
		root := portTree(t, tc.manifest, tc.files)
		probs, _, err := portCheck(root)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if !strings.Contains(strings.Join(probs, "\n"), tc.want) {
			t.Errorf("%s: the problems %v do not have %q", tc.name, probs, tc.want)
		}
	}
}

func TestPortExitCodes(t *testing.T) {
	root := portTree(t, goodManifest, map[string]string{"MinimalModel/Ported/Src/A.lean": goodFile})
	for _, tc := range []struct {
		args []string
		code int
	}{
		{[]string{"port"}, 2},
		{[]string{"port", "nothing"}, 2},
		{[]string{"port", "check", "-lean", root}, 0},
		{[]string{"port", "check", "-lean", "/does/not/exist"}, 1},
		{[]string{"port", "add"}, 1},
	} {
		var out, errb strings.Builder
		if got := run(tc.args, &out, &errb); got != tc.code {
			t.Errorf("mmp %v: exit %d, want %d (%s)", tc.args, got, tc.code, errb.String())
		}
	}
}
