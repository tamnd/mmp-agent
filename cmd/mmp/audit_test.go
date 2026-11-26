package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const auditJSON = `{"source": "AxiomMath/MMP", "commit": "1f9ade3670", "steps": {"A1": "in progress"}, "findings": {"1": 0}, "passed": false}`

func auditTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "records", "audit.json"), auditJSON)
	writeFile(t, filepath.Join(root, "audit", "axiom-mmp", "A1-inventory.md"), "# A1\n")
	return root
}

func readAuditFile(t *testing.T, root string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "records", "audit.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestAuditStatus(t *testing.T) {
	root := auditTree(t)
	var out, errb bytes.Buffer
	if code := run([]string{"audit", "status", "-content", root}, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	for _, want := range []string{"AxiomMath/MMP at 1f9ade3670", "A1  in progress  audit/axiom-mmp/A1-inventory.md", "A8  not started", "passed: no"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the status has no %q:\n%s", want, out.String())
		}
	}
}

func TestAuditStep(t *testing.T) {
	root := auditTree(t)
	var out, errb bytes.Buffer
	if code := run([]string{"audit", "step", "-content", root, "A2", "done"}, &out, &errb); code != 1 || !strings.Contains(errb.String(), "no exit record") {
		t.Fatalf("a step with no exit record is done: exit %d, %s", code, errb.String())
	}
	if code := run([]string{"audit", "step", "-content", root, "A9", "done"}, &out, &errb); code != 2 {
		t.Fatalf("the step A9 is accepted: exit %d", code)
	}
	if code := run([]string{"audit", "step", "-content", root, "A1", "finished"}, &out, &errb); code != 2 {
		t.Fatalf("the state finished is accepted: exit %d", code)
	}
	errb.Reset()
	if code := run([]string{"audit", "step", "-content", root, "-date", "2026-11-27", "a1", "done"}, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	m := readAuditFile(t, root)
	if m["steps"].(map[string]any)["A1"] != "done" || m["date"] != "2026-11-27" || m["passed"] != false {
		t.Errorf("records/audit.json is %v", m)
	}
}

func TestAuditFinding(t *testing.T) {
	root := auditTree(t)
	var out, errb bytes.Buffer
	if code := run([]string{"audit", "finding", "-content", root, "-severity", "2", "close"}, &out, &errb); code != 1 {
		t.Fatalf("a finding that is not open is closed: exit %d", code)
	}
	if code := run([]string{"audit", "finding", "-content", root, "-severity", "5", "add"}, &out, &errb); code != 2 {
		t.Fatalf("the severity 5 is accepted: exit %d", code)
	}
	for range 2 {
		if code := run([]string{"audit", "finding", "-content", root, "-severity", "2", "add"}, &out, &errb); code != 0 {
			t.Fatalf("exit %d: %s", code, errb.String())
		}
	}
	if code := run([]string{"audit", "finding", "-content", root, "-severity", "2", "close"}, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if got := readAuditFile(t, root)["findings"].(map[string]any)["2"]; got != 1.0 {
		t.Errorf("the open findings of severity 2 are %v, not 1", got)
	}
}

func TestParseAxioms(t *testing.T) {
	out := "'A.b' depends on axioms: [propext,\n Classical.choice,\n Quot.sound]\n'A.c' does not depend on any axioms\n'A.d' depends on axioms: [sorryAx]\n"
	got := parseAxioms(out)
	if len(got) != 3 || len(got["A.b"]) != 3 || got["A.b"][1] != "Classical.choice" || len(got["A.c"]) != 0 || got["A.d"][0] != "sorryAx" {
		t.Errorf("parseAxioms gives %v", got)
	}
}

func TestAuditAxioms(t *testing.T) {
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "lean-toolchain"), "leanprover/lean4:v4.35.0\n")
	var file string
	old := runLean
	defer func() { runLean = old }()
	runLean = func(_, f string) ([]byte, error) {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		file = string(data)
		return []byte("'A.good' depends on axioms: [propext, Quot.sound]\n'A.bad' depends on axioms: [propext, A.myAxiom]\n"), nil
	}
	report := filepath.Join(t.TempDir(), "A2.json")
	var out, errb bytes.Buffer
	if code := run([]string{"audit", "axioms", "-source", src, "-decl", "A.good", "-json", report}, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if !strings.HasPrefix(file, "import MMP\n") || !strings.Contains(file, "#print axioms A.good\n") {
		t.Errorf("the Lean file is %q", file)
	}
	var rep AxiomReport
	data, err := os.ReadFile(report)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Toolchain != "leanprover/lean4:v4.35.0" || len(rep.Rows) != 1 || !rep.Rows[0].Standard {
		t.Errorf("the report is %+v", rep)
	}
	errb.Reset()
	if code := run([]string{"audit", "axioms", "-source", src, "-decl", "A.good", "-decl", "A.bad", "-decl", "A.none"}, &out, &errb); code != 1 || !strings.Contains(errb.String(), "2 declarations of 3") {
		t.Fatalf("a bad axiom passes: exit %d, %s", code, errb.String())
	}
	runLean = func(string, string) ([]byte, error) {
		return []byte("error: unknown package"), errors.New("exit status 1")
	}
	if code := run([]string{"audit", "axioms", "-source", src}, &out, &errb); code != 1 {
		t.Fatalf("a failed run of Lean passes: exit %d", code)
	}
}
