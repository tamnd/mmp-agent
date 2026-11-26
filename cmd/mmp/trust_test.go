package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tamnd/mmp-agent/blueprint"
)

const cleanAxioms = `["propext","Classical.choice","Quot.sound"]`

// trustTree writes a content repository with a proved top theorem, a stated node with sorry and a proved-mod node. The argument extra adds declarations to records/lean.json, and commit is the commit of the record.
func trustTree(t *testing.T, commit, extra string) string {
	t.Helper()
	root := t.TempDir()
	node := func(tag, body string) {
		writeFile(t, filepath.Join(root, "blueprint", "nodes", tag+".yaml"), "tag: "+tag+"\nkind: theorem\ntitle: t\nstatement: s\ntier: T3\n"+body)
	}
	node("MM00000001", "lean:\n  statement: MinimalModel.Castelnuovo\n  proof: MinimalModel.Surface.castelnuovo\n")
	node("MM00000002", "lean:\n  statement: MinimalModel.LocalExistence\n  proof: MinimalModel.localExistence\n")
	node("MM00000003", "hypotheses: [MM00000002]\nlean:\n  statement: MinimalModel.Mod\n  proof: MinimalModel.mod\n")
	writeFile(t, filepath.Join(root, "records", "lean.json"), `{"repository":"https://github.com/tamnd/mmp-lean","commit":"`+commit+`","toolchain":"4.35.0","mathlib":"v4.35.0","built":"2026-11-25","decls":{
"MinimalModel.Castelnuovo":{"kind":"def","axioms":`+cleanAxioms+`},
"MinimalModel.Surface.castelnuovo":{"kind":"theorem","axioms":`+cleanAxioms+`},
"MinimalModel.LocalExistence":{"kind":"def","axioms":[]},
"MinimalModel.localExistence":{"kind":"theorem","axioms":["propext","sorryAx"]},
"MinimalModel.Mod":{"kind":"def","axioms":[]},
"MinimalModel.mod":{"kind":"theorem","axioms":["propext"]}`+extra+`}}`)
	return root
}

// replayJSON is a replay record of commit with the checkers given as name=ok pairs. A name that starts with "kernel" is not independent.
func replayJSON(commit string, comparator string, checkers ...string) string {
	var cs []string
	for _, c := range checkers {
		name, ok, _ := strings.Cut(c, "=")
		ind := "true"
		if strings.HasPrefix(name, "kernel") {
			ind = "false"
		}
		cs = append(cs, `{"name":"`+name+`","independent":`+ind+`,"ok":`+ok+`}`)
	}
	s := `{"commit":"` + commit + `","date":"2026-11-25","checkers":[` + strings.Join(cs, ",") + `]`
	if comparator != "" {
		s += `,"comparator":{"ok":` + comparator + `}`
	}
	return s + "}"
}

type trustFile struct {
	Updated string `json:"updated"`
	Replay  string `json:"replay"`
	Checks  []struct {
		ID, State, Note string
	} `json:"checks"`
	Theorems []struct {
		Theorem, Node, Status string
		Checks                []struct{ ID, State string }
	} `json:"theorems"`
	Decls []struct {
		Name  string   `json:"name"`
		Other []string `json:"other_axioms"`
	} `json:"declarations"`
}

func readTrust(t *testing.T, root string) (trustFile, map[string]string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "records", "trust.json"))
	if err != nil {
		t.Fatal(err)
	}
	var f trustFile
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatal(err)
	}
	states := map[string]string{}
	for _, c := range f.Checks {
		states[c.ID] = c.State
	}
	return f, states
}

func TestTrust(t *testing.T) {
	for _, tc := range []struct {
		name   string
		extra  string // more declarations of records/lean.json
		replay string // records/replay.json, when not empty
		noLean bool   // remove records/lean.json
		args   []string
		code   int
		out    string
		want   map[string]string // the states of some rows
		note   string            // a part of the note of a row, as "Q3 text"
	}{
		{name: "no replay", code: 0, out: "Q3  not run", want: map[string]string{"Q0": "not run", "Q1": "yes", "Q2": "yes", "Q3": "not run", "Q4": "not run", "Q5": "open", "Q6": "open", "Q7": "not run", "Q8": "not run"}, note: "Q3 The kernel replay was not run"},
		{name: "no lean record", noLean: true, code: 0, want: map[string]string{"Q0": "not run", "Q1": "not run", "Q2": "not run", "Q3": "not run"}},
		{name: "an axiom that is not standard", extra: `,"MinimalModel.mod":{"kind":"theorem","axioms":["MinimalModel.cheat"]}`, code: 1, out: "Q2  no", want: map[string]string{"Q2": "no"}, note: "Q2 MinimalModel.mod uses MinimalModel.cheat"},
		{name: "a replay with lean4lean", replay: replayJSON(goodCommit, "", "kernel=true", "lean4lean=true"), code: 0, want: map[string]string{"Q3": "yes", "Q4": "not run"}, note: "Q3 kernel, lean4lean accepted"},
		{name: "only the kernel replayed", replay: replayJSON(goodCommit, "", "kernel=true"), code: 0, want: map[string]string{"Q3": "not run"}, note: "Q3 No independent checker ran"},
		{name: "a checker failed", replay: replayJSON(goodCommit, "", "kernel=true", "nanoda=false"), code: 1, want: map[string]string{"Q3": "no"}, note: "Q3 failed in nanoda"},
		{name: "a replay of another commit", replay: replayJSON("1111111111111111111111111111111111111111", "true", "lean4lean=true"), code: 0, want: map[string]string{"Q3": "not run", "Q4": "not run"}, note: "Q3 is of commit 1111111"},
		{name: "quick skips the replay", replay: replayJSON(goodCommit, "true", "lean4lean=true"), args: []string{"-quick"}, code: 0, want: map[string]string{"Q3": "not run", "Q4": "not run"}, note: "Q3 -quick"},
		{name: "the comparator passed", replay: replayJSON(goodCommit, "true", "lean4lean=true"), code: 0, want: map[string]string{"Q3": "yes", "Q4": "yes"}},
		{name: "the comparator failed", replay: replayJSON(goodCommit, "false", "lean4lean=true"), code: 1, want: map[string]string{"Q4": "no"}},
		{name: "a replay with an unknown field", replay: `{"commit":"x","checkers":[],"frob":1}`, code: 1},
		{name: "a missing replay file that the user names", args: []string{"-replay", "/does/not/exist.json"}, code: 1},
		{name: "a bad date", args: []string{"-date", "25 November"}, code: 2},
		{name: "an argument", args: []string{"extra"}, code: 2},
	} {
		root := trustTree(t, goodCommit, tc.extra)
		if tc.noLean {
			if err := os.Remove(filepath.Join(root, "records", "lean.json")); err != nil {
				t.Fatal(err)
			}
		}
		if tc.replay != "" {
			writeFile(t, filepath.Join(root, "records", "replay.json"), tc.replay)
		}
		args := append([]string{"trust", "-content", root, "-date", "2026-11-25"}, tc.args...)
		var out, errb strings.Builder
		if got := run(args, &out, &errb); got != tc.code {
			t.Errorf("%s: exit %d, want %d (%s)", tc.name, got, tc.code, errb.String())
			continue
		}
		if !strings.Contains(out.String(), tc.out) {
			t.Errorf("%s: the output does not have %q:\n%s", tc.name, tc.out, out.String())
		}
		if tc.want == nil {
			continue
		}
		f, states := readTrust(t, root)
		for id, want := range tc.want {
			if states[id] != want {
				t.Errorf("%s: %s is %q, want %q", tc.name, id, states[id], want)
			}
		}
		if tc.note != "" {
			id, text, _ := strings.Cut(tc.note, " ")
			found := false
			for _, c := range f.Checks {
				found = found || c.ID == id && strings.Contains(c.Note, text)
			}
			if !found {
				t.Errorf("%s: the note of %s does not have %q: %+v", tc.name, id, text, f.Checks)
			}
		}
		if f.Updated != "2026-11-25" || len(f.Checks) != 11 {
			t.Errorf("%s: the report has the date %q and %d rows", tc.name, f.Updated, len(f.Checks))
		}
	}
}

func TestTrustTopTheorems(t *testing.T) {
	root := trustTree(t, goodCommit, "")
	writeFile(t, filepath.Join(root, "records", "fidelity.json"), `{"MinimalModel.Castelnuovo":{"class":"theorem","checks":{"F1":true,"F2":true,"F3":true,"F4":true,"F5":true},"pass":false}}`)
	var out, errb strings.Builder
	if got := run([]string{"trust", "-content", root}, &out, &errb); got != 0 {
		t.Fatalf("exit %d (%s)", got, errb.String())
	}
	f, _ := readTrust(t, root)
	if f.Replay != "none" {
		t.Errorf("the replay is %q, want none", f.Replay)
	}
	found := false
	for _, th := range f.Theorems {
		if th.Theorem != "MinimalModel.Surface.castelnuovo" {
			continue
		}
		found = true
		if th.Node != "MM00000001" || th.Status != "proved" || th.Checks[0].State != "yes" || th.Checks[1].State != "open" {
			t.Errorf("the row of castelnuovo is %+v", th)
		}
	}
	if !found || !strings.Contains(out.String(), "MinimalModel.Surface.castelnuovo  S5  MM00000001 proved  Q2 yes, Q5 open") {
		t.Errorf("no row for castelnuovo:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "MM00000003 takes MM00000002") {
		t.Errorf("Q6 does not list the hypothesis:\n%s", out.String())
	}
}

// leanRepo writes a git repository like mmp-lean with two statement files and their manifest. It returns the root and the commit.
func leanRepo(t *testing.T, manifest func(sums map[string]string) string) (string, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	sums := map[string]string{}
	for _, name := range []string{"Basic", "ClassField"} {
		text := "namespace MinimalModel\n-- " + name + "\nend MinimalModel\n"
		writeFile(t, filepath.Join(root, "MinimalModel", "Statements", name+".lean"), text)
		h := sha256.Sum256([]byte(text))
		sums["MinimalModel/Statements/"+name+".lean"] = hex.EncodeToString(h[:])
	}
	writeFile(t, filepath.Join(root, "manifests", "statements.sha256"), manifest(sums))
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "-m", "init"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	head, err := gitHead(root)
	if err != nil {
		t.Fatal(err)
	}
	return root, head
}

func TestTrustLeanCheckout(t *testing.T) {
	good := func(sums map[string]string) string {
		return sums["MinimalModel/Statements/Basic.lean"] + "  MinimalModel/Statements/Basic.lean\n" + sums["MinimalModel/Statements/ClassField.lean"] + "  MinimalModel/Statements/ClassField.lean\n"
	}
	for _, tc := range []struct {
		name     string
		manifest func(map[string]string) string
		same     bool // the record is of the commit of the checkout
		code     int
		q0, q4   string
		note     string
	}{
		{"same commit and good hashes", good, true, 0, "yes", "not run", "match manifests/statements.sha256"},
		{"another commit", good, false, 1, "no", "not run", ""},
		{"a changed statement file", func(s map[string]string) string {
			return strings.Repeat("0", 64) + "  MinimalModel/Statements/Basic.lean\n" + s["MinimalModel/Statements/ClassField.lean"] + "  MinimalModel/Statements/ClassField.lean\n"
		}, true, 1, "yes", "no", "Basic.lean does not have the hash"},
		{"a file that is not in the manifest", func(s map[string]string) string {
			return s["MinimalModel/Statements/Basic.lean"] + "  MinimalModel/Statements/Basic.lean\n"
		}, true, 1, "yes", "no", "ClassField.lean is not in manifests/statements.sha256"},
		{"a missing file", func(s map[string]string) string {
			return good(s) + strings.Repeat("1", 64) + "  MinimalModel/Statements/Gone.lean\n"
		}, true, 1, "yes", "no", "Gone.lean is in manifests/statements.sha256 but cannot be read"},
	} {
		lean, head := leanRepo(t, tc.manifest)
		commit := head
		if !tc.same {
			commit = goodCommit
		}
		root := trustTree(t, commit, "")
		var out, errb strings.Builder
		if got := run([]string{"trust", "-content", root, "-lean", lean}, &out, &errb); got != tc.code {
			t.Errorf("%s: exit %d, want %d (%s)", tc.name, got, tc.code, errb.String())
			continue
		}
		f, states := readTrust(t, root)
		if states["Q0"] != tc.q0 || states["Q4"] != tc.q4 {
			t.Errorf("%s: Q0 %q and Q4 %q, want %q and %q", tc.name, states["Q0"], states["Q4"], tc.q0, tc.q4)
		}
		if tc.note != "" && !strings.Contains(f.Checks[4].Note, tc.note) {
			t.Errorf("%s: the note of Q4 is %q, want %q", tc.name, f.Checks[4].Note, tc.note)
		}
	}
	var out, errb strings.Builder
	if got := run([]string{"trust", "-content", trustTree(t, goodCommit, ""), "-lean", t.TempDir()}, &out, &errb); got != 1 {
		t.Errorf("a directory that is not a git checkout: exit %d, want 1", got)
	}
}

func TestAuditRow(t *testing.T) {
	steps := func(n int) map[string]string {
		m := map[string]string{}
		for i, s := range blueprint.AuditSteps {
			if i < n {
				m[s] = "done"
			} else {
				m[s] = "open"
			}
		}
		return m
	}
	for _, tc := range []struct {
		name  string
		audit *blueprint.Audit
		state string
		text  string
	}{
		{"no record", nil, answerNotRun, "has not started"},
		{"no step", &blueprint.Audit{Source: "AxiomMath/MMP", Commit: "1f9ade3670", Steps: steps(0)}, answerOpen, "no step done"},
		{"two steps", &blueprint.Audit{Source: "AxiomMath/MMP", Commit: "1f9ade3670", Steps: steps(2)}, answerOpen, "the steps A1, A2 done"},
		{"a serious finding", &blueprint.Audit{Source: "AxiomMath/MMP", Commit: "1f9ade3670", Steps: steps(8), Findings: map[string]int{"1": 1}}, answerNo, "1 open finding of severity 1 or 2"},
		{"done but not passed", &blueprint.Audit{Source: "AxiomMath/MMP", Commit: "1f9ade3670", Steps: steps(8)}, answerOpen, "the steps A1, A2, A3, A4, A5, A6, A7, A8 done"},
		{"passed", &blueprint.Audit{Source: "AxiomMath/MMP", Commit: "1f9ade3670", Steps: steps(8), Findings: map[string]int{"3": 2}, Passed: true}, answerYes, "passed"},
	} {
		state, note := auditRow(&blueprint.Project{Audit: tc.audit})
		if state != tc.state || !strings.Contains(note, tc.text) {
			t.Errorf("%s: the row is %q, %q; want %q with %q", tc.name, state, note, tc.state, tc.text)
		}
	}
}
