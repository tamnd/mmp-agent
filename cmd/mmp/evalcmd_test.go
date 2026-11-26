package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// evalTree writes two statements and two definitions, one of them planted.
func evalTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, id := range []string{"S001", "S002"} {
		writeFile(t, filepath.Join(root, "eval", "statements", id+".yaml"), "id: "+id+"\nname: X\ninformal: |\n  every pair\nfaithful: |\n  ∀ A, P A\nplanted: |\n  ∃ A, P A\ndefect: \"some in place of every\"\n")
	}
	writeFile(t, filepath.Join(root, "eval", "audit", "A001.yaml"), "id: A001\nreference: |\n  positive\nlean: |\n  0 ≤ x\nplanted: true\ndefect: \"weak\"\n")
	writeFile(t, filepath.Join(root, "eval", "audit", "A002.yaml"), "id: A002\nreference: |\n  positive\nlean: |\n  0 < x\nplanted: false\n")
	return root
}

func readEval(t *testing.T, path string) evalRecord {
	t.Helper()
	data, err := os.ReadFile(path) // #nosec G304 -- a test file
	if err != nil {
		t.Fatal(err)
	}
	var r evalRecord
	if err := json.Unmarshal(data, &r); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestAgentEval(t *testing.T) {
	root := evalTree(t)
	out := mmp(t, 0, "agent", "eval", "-content", root, "-runner", "constant:faithful", "-date", "2026-11-26", "-label", "base")
	if !strings.Contains(out, "statements\t2/4\t50.0%") || !strings.Contains(out, "audit\t1/2\t50.0%") {
		t.Errorf("the constant baseline is wrong:\n%s", out)
	}
	base := filepath.Join(root, "records", "eval", "2026-11-26-base.json")
	if r := readEval(t, base); !r.Pass || r.Sets["statements"].Rate != 50 || r.Answers != "2026-11-26-base.jsonl" {
		t.Errorf("the record is wrong: %+v", r)
	}

	// A runner that finds each planted text: it reads the prompt and answers from the Lean text. It writes one ledger line per question.
	bin := t.TempDir()
	good := filepath.Join(bin, "good.sh")
	script := `#!/bin/sh
p=$(cat)
echo "{\"run\":\"$LGL_RUN_ID\",\"role\":\"$LGL_ROLE\",\"model\":\"$LGL_MODEL\",\"prompt_version\":\"eval-1\",\"tokens_in\":10,\"tokens_out\":5,\"cost\":0.25}" >> agent/ledger.jsonl
echo "some reasons"
echo "verdict: faithful"
case "$p" in
  *"∃ A"*) echo "verdict: wrong" ;;
  *"0 ≤ x"*) echo "verdict: planted" ;;
esac
`
	if err := os.MkdirAll(filepath.Join(root, "agent"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(good, []byte(script), 0o700); err != nil { // #nosec G306 -- a test script
		t.Fatal(err)
	}
	out = mmp(t, 0, "agent", "eval", "-content", root, "-runner", good, "-date", "2026-11-26", "-label", "good", "-baseline", base)
	if !strings.Contains(out, "statements\t4/4\t100.0%\t+50.0 points") || !strings.Contains(out, "cost\t$1.50") {
		t.Errorf("the run of the good runner is wrong:\n%s", out)
	}
	if r := readEval(t, filepath.Join(root, "records", "eval", "2026-11-26-good.json")); !r.Pass || r.Cost != 1.5 || r.Model["reader"] != "claude-opus-5-5" {
		t.Errorf("the record is wrong: %+v", r)
	}

	// Against the good run, the constant baseline drops 50 points, and the gate of spec 10.9 fails.
	out = mmp(t, 1, "agent", "eval", "-content", root, "-runner", "constant:faithful", "-date", "2026-11-26", "-label", "worse", "-baseline", filepath.Join(root, "records", "eval", "2026-11-26-good.json"))
	if !strings.Contains(out, "more than 5 points") {
		t.Errorf("the gate did not fail:\n%s", out)
	}

	// A trial writes no record.
	out = mmp(t, 0, "agent", "eval", "-content", root, "-runner", "constant:wrong", "-set", "statements", "-limit", "1", "-label", "trial")
	if !strings.Contains(out, "no record is written") {
		t.Errorf("the trial wrote a record:\n%s", out)
	}
	mmp(t, 2, "agent", "eval", "-content", root, "-set", "tasks", "-runner", "constant:faithful")
}
