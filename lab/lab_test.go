package lab

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunE2Dimension3(t *testing.T) {
	r, err := RunE2(E2Inputs{Dimension: 3, MaxIndex: 30})
	if err != nil {
		t.Fatal(err)
	}
	if r.Mismatches() != 0 || r.Gaps() != 0 {
		t.Fatalf("the run contradicts the terminal lemma: %v %v %v", r.LemmaFails, r.MldFails, r.NoTerminal)
	}
	// 1/2(1,1,1) is terminal with mld 3/2. 1/3(1,1,1) is canonical but not terminal, with mld 1. 1/5(1,1,1) has mld 3/5.
	want := map[string][2]int{"1/2[1 1 1]": {3, 2}, "1/3[1 1 1]": {1, 1}, "1/5[1 1 1]": {3, 5}, "1/7[1 2 4]": {1, 1}}
	found := 0
	for _, g := range r.Germs {
		k := fmt.Sprintf("1/%d%v", g.Index, g.Weights)
		if m, ok := want[k]; ok {
			found++
			if g.MldNum != m[0] || g.MldDen != m[1] {
				t.Errorf("%s has the mld %d/%d, want %d/%d", k, g.MldNum, g.MldDen, m[0], m[1])
			}
		}
	}
	if found != len(want) {
		t.Errorf("the run has %d of the %d known germs", found, len(want))
	}
	for _, x := range r.Indices {
		if x.Index == 2 && (x.Germs != 1 || x.Terminal != 1) {
			t.Errorf("index 2: %+v", x)
		}
		if x.Index == 3 && (x.Germs != 2 || x.Terminal != 1 || x.Canonical != 2) {
			t.Errorf("index 3: %+v, want the germs 1/3(1,1,1) and 1/3(1,1,2)", x)
		}
	}
	if r.Values[0] != "1/10" {
		t.Errorf("the smallest mld up to index 30 is %s", r.Values[0])
	}
}

func TestRunE2Dimension4(t *testing.T) {
	r, err := RunE2(E2Inputs{Dimension: 4, MaxIndex: 12})
	if err != nil {
		t.Fatal(err)
	}
	if r.Mismatches() != 0 || r.Gaps() != 0 || len(r.Germs) == 0 {
		t.Fatalf("a run of dimension 4 has no prediction: %d %d %d", r.Mismatches(), r.Gaps(), len(r.Germs))
	}
	for _, g := range r.Germs {
		if g.Lemma != nil {
			t.Fatalf("a germ of dimension 4 has the lemma field: %+v", g)
		}
	}
}

func TestWrongPredictionIsCaught(t *testing.T) {
	g := germ([]int{1, 1, 1}, 5)
	if g.Terminal || *g.Lemma {
		t.Fatalf("1/5(1,1,1) is %+v", g)
	}
	g = germ([]int{1, 2, 4}, 5)
	if !g.Terminal || !*g.Lemma || g.MldNum != 6 || g.MldDen != 5 {
		t.Fatalf("1/5(1,2,4) is %+v, want terminal with mld 6/5", g)
	}
	r := E2Result{LemmaFails: []Germ{g}}
	if r.Mismatches() != 1 || !strings.HasPrefix(e2Result(r), "mismatch") {
		t.Errorf("a failed check gives %q", e2Result(r))
	}
}

func TestE2Inputs(t *testing.T) {
	for _, tc := range []struct {
		in   E2Inputs
		want string
	}{
		{E2Inputs{Dimension: 3, MaxIndex: 10}, ""},
		{E2Inputs{Dimension: 5, MaxIndex: 10}, "dimension"},
		{E2Inputs{Dimension: 3, MaxIndex: 1}, "at least 2"},
		{E2Inputs{Dimension: 3, MaxIndex: 1000}, "maximum index"},
		{E2Inputs{Dimension: 4, MaxIndex: 100}, "maximum index"},
	} {
		err := tc.in.check()
		if (tc.want == "") != (err == nil) || err != nil && !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%+v: the error is %v, want %q", tc.in, err, tc.want)
		}
	}
}

func TestParseSpec(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(s string) string
		want string
	}{
		{"the spec of E2", func(s string) string { return s }, ""},
		{"a bad name", func(s string) string { return strings.Replace(s, "name: e2-cyclic", "name: E2 cyclic", 1) }, "the name"},
		{"an unknown experiment", func(s string) string { return strings.Replace(s, "experiment: E2", "experiment: E11", 1) }, "E1 to E10"},
		{"an experiment with no code", func(s string) string { return strings.Replace(s, "experiment: E2", "experiment: E1", 1) }, "no code for experiment E1"},
		{"another tool", func(s string) string { return strings.Replace(s, "tool: mmp", "tool: mmp-lab", 1) }, "the tool"},
		{"no node", func(s string) string {
			return strings.Replace(s, "node: MMXCD16PTN # HanLiuEncACC, Target T", "node: \"\"", 1)
		}, "a node"},
		{"an unknown field", func(s string) string { return s + "image: pari\n" }, "image"},
		{"bad inputs", func(s string) string { return strings.Replace(s, "dimension: 3", "dimension: 7", 1) }, "dimension"},
	} {
		s, err := ParseSpec("lab/specs/e2.yaml", []byte(tc.edit(SpecE2)))
		if tc.want == "" {
			if err != nil {
				t.Errorf("%s: %v", tc.name, err)
			} else if s.Inputs.MaxIndex != 60 || len(s.Hash) != 64 {
				t.Errorf("%s: the spec is %+v", tc.name, s)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: the error is %v, want %q", tc.name, err, tc.want)
		}
	}
}

const smallSpec = `name: e2-small
experiment: E2
question: Do the small cases agree?
tool: mmp
node: MM00000001
inputs: {dimension: 3, max_index: 12}
outputs: [germs.json, indices.json, summary.md]
`

func TestRun(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "lab", "specs", "e2-small.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(smallSpec), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := LoadSpec(path)
	if err != nil {
		t.Fatal(err)
	}
	runs := RunsDir(path)
	if runs != filepath.Join(root, "lab", "runs") {
		t.Fatalf("the runs directory is %q", runs)
	}
	now := func() time.Time { return time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC) }
	opt := Options{Runs: runs, Host: "test", ToolVersion: "dev", Now: now}
	rec, err := Run(s, opt)
	if err != nil {
		t.Fatal(err)
	}
	if want := "2026-11-25-e2-small-" + s.Hash[:8]; rec.ID != want {
		t.Errorf("the id is %q, want %q", rec.ID, want)
	}
	if rec.Spec != "lab/specs/e2-small.yaml" || rec.Container != "none" || rec.Network || rec.Mismatches != 0 || !strings.HasPrefix(rec.Result, "agrees") {
		t.Errorf("the record is %+v", rec)
	}
	dir := filepath.Join(runs, rec.ID)
	for name, sum := range rec.Files {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		h := sha256.Sum256(data)
		if hex.EncodeToString(h[:]) != sum {
			t.Errorf("%s does not have the hash of the record", name)
		}
	}
	if len(rec.Files) != 3 {
		t.Errorf("the run has the files %v", rec.Files)
	}
	summary, _ := os.ReadFile(filepath.Join(dir, "summary.md"))
	if !strings.Contains(string(summary), "## What the run does not do") {
		t.Errorf("the summary does not say what the run does not do:\n%s", summary)
	}
	if _, err := Run(s, opt); err == nil || !strings.Contains(err.Error(), "-force") {
		t.Errorf("a second run gives %v, want an error about -force", err)
	}
	opt.Force = true
	again, err := Run(s, opt)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"germs.json", "indices.json", "summary.md"} {
		if again.Files[name] != rec.Files[name] {
			t.Errorf("%s changed between two runs of the same spec", name)
		}
	}
	got, err := LoadRuns(runs)
	if err != nil || len(got) != 1 || got[0].ID != rec.ID {
		t.Errorf("LoadRuns gives %v, %v", got, err)
	}
	if err := os.Rename(dir, filepath.Join(runs, "other")); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRuns(runs); err == nil {
		t.Error("LoadRuns reads a run whose id is not the name of its directory")
	}
	if runs, err := LoadRuns(filepath.Join(root, "nothing")); err != nil || runs != nil {
		t.Errorf("a missing directory gives %v, %v", runs, err)
	}
}
