package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// costTree writes a content repository with two nodes and an audit record:
//   - MM00000001 is proved;
//   - MM00000002 is stated, so it is work in progress.
func costTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	node := func(tag, title, extra string) {
		writeFile(t, filepath.Join(root, "blueprint", "nodes", tag+".yaml"), "tag: "+tag+"\nkind: theorem\ntitle: "+title+"\nstatement: the statement of "+tag+"\ntier: T3\n"+extra)
	}
	node("MM00000001", "A proved theorem", "route: [R3]\nlean:\n  statement: MinimalModel.A\n  proof: MinimalModel.a\n")
	node("MM00000002", "A stated theorem", "lean:\n  statement: MinimalModel.B\n")
	writeFile(t, filepath.Join(root, "records", "lean.json"), `{"commit":"`+goodCommit+`","decls":{
"MinimalModel.A":{"kind":"def","axioms":[]},
"MinimalModel.a":{"kind":"theorem","axioms":["propext"]},
"MinimalModel.B":{"kind":"def","axioms":[]}}}`)
	writeFile(t, filepath.Join(root, "records", "audit.json"), auditJSON)
	return root
}

// costRun writes a run record.
func costRun(t *testing.T, root, id, role, target, kind, route, date string) {
	t.Helper()
	writeFile(t, filepath.Join(root, "records", "runs", id, "run.json"), fmt.Sprintf(`{"id":%q,"role":%q,"target":%q,"kind":%q,"title":"t","route":%q,"date":%q,"status":"ran","model":"m","model_from":"agent.yaml","prompt_version":"mmp-1","prompt_sha256":"","budget_asked":5,"budget":5,"branch":"b","rank":1,"reason":"r","checks":"build","inputs":{"files":[]},"tool":"mmp","tool_version":"dev"}`, id, role, target, kind, route, date))
}

func ledgerLine(date, run, role string, cost float64) string {
	d := ""
	if date != "" {
		d = fmt.Sprintf(`"date":%q,`, date)
	}
	return fmt.Sprintf(`{%s"run":%q,"role":%q,"model":"m","prompt_version":"mmp-1","tokens_in":10,"tokens_out":5,"cost":%g}`+"\n", d, run, role, cost)
}

// costRecords writes the runs and the ledger of the tests.
func costRecords(t *testing.T, root string) {
	t.Helper()
	costRun(t, root, "p1", "prover", "MM00000001", "node", "R3", "2026-11-10")
	costRun(t, root, "p2", "prover", "MM00000002", "node", "library", "2026-11-12")
	costRun(t, root, "a1", "auditor", "MMP.Mori.IsKltPair", "node", "R2", "2026-11-14")
	costRun(t, root, "c1", "reader", "arXiv-2611-31396", "claim", "R1", "2026-11-14")
	writeFile(t, filepath.Join(root, "agent", "ledger.jsonl"),
		ledgerLine("", "p1", "prover", 1)+ // the date of the run
			ledgerLine("2026-11-11", "p1", "prover", 0.5)+
			ledgerLine("2026-11-12", "p2", "prover", 2)+
			ledgerLine("2026-11-14", "a1", "auditor", 3)+
			ledgerLine("2026-11-14", "c1", "reader", 0.75)+
			ledgerLine("2026-11-15", "gone", "prover", 0.25)+ // no run record
			ledgerLine("2026-10-01", "p1", "prover", 100)+ // before the window
			ledgerLine("", "nowhere", "prover", 7)) // no date and no run
}

func TestAgentCost(t *testing.T) {
	for _, tc := range []struct {
		name    string
		records bool // write the runs and the ledger
		empty   bool // write an empty ledger
		args    []string
		want    []string
		notWant []string
	}{
		{
			name: "no ledger",
			args: []string{"-to", "2026-11-30"},
			want: []string{
				"# The cost report to 2026-11-30\n",
				"from the first entry of the ledger to 2026-11-30",
				"There is no ledger agent/ledger.jsonl, so no spend is recorded.",
				"The map has 1 proved node. No run of the window was on a proved node",
				"The role auditor spent 0.00 dollars",
				"Step A5, the reading of each definition of the index of A4, is not started there.",
			},
			notWant: []string{"| Role |"},
		},
		{
			name:  "an empty ledger",
			empty: true,
			args:  []string{"-to", "2026-11-30"},
			want:  []string{"The ledger has no model call in the window, so the spend is 0.00 dollars."},
		},
		{
			name:    "the window",
			records: true,
			args:    []string{"-from", "2026-11-01", "-to", "2026-11-30"},
			want: []string{
				"from 2026-11-01 to 2026-11-30",
				"The ledger has 6 model calls in the window, for 7.50 dollars.",
				"| auditor | 3.00 |\n| prover | 3.75 |\n| reader | 0.75 |\n",
				"| R1 | 0.75 |\n| R2 | 3.00 |\n| R3 | 1.50 |\n| library | 2.00 |\n| unknown | 0.25 |\n",
				"| a node that is proved now | 2 | 1.50 |",
				"| a node that is not proved yet | 1 | 2.00 |",
				"| a claim | 1 | 0.75 |",
				"| a node that is not in the map now | 1 | 3.00 |",
				"| unattributed: the run has no record | 1 | 0.25 |",
				"| total | 6 | 7.50 |",
				"The runs of the window on 1 of them spent 1.50 dollars, so the cost per proved node is 1.50 dollars.",
				"| MM00000001 | A proved theorem | 2 | 1.50 |",
				"on nodes that are not proved yet spent 2.00 dollars, on 1 node.",
				"| MM00000002 | A stated theorem | stated | 1 | 2.00 |",
				"The role auditor spent 3.00 dollars in the window, in 1 model call.",
				"the number of audited definitions in the window is 0",
				"The ledger also has 1 model call with no date, for 7.00 dollars",
			},
			notWant: []string{"100.00", "107.50"},
		},
		{
			name:    "no lower bound",
			records: true,
			args:    []string{"-to", "2026-11-30"},
			want:    []string{"The ledger has 7 model calls in the window, for 107.50 dollars.", "| a node that is proved now | 3 | 101.50 |", "| total | 7 | 107.50 |"},
		},
		{
			name:    "a window with only the unattributed call",
			records: true,
			args:    []string{"-from", "2026-11-15", "-to", "2026-11-15"},
			want: []string{
				"The ledger has 1 model call in the window, for 0.25 dollars.",
				"| unknown | 0.25 |",
				"| unattributed: the run has no record | 1 | 0.25 |",
				"No run of the window was on a proved node",
				"No run of the window was on a node that is not proved yet.",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := costTree(t)
			if tc.records {
				costRecords(t, root)
			}
			if tc.empty {
				writeFile(t, filepath.Join(root, "agent", "ledger.jsonl"), "")
			}
			out := mmp(t, 0, append([]string{"agent", "cost", "-content", root}, tc.args...)...)
			for _, w := range tc.want {
				if !strings.Contains(out, w) {
					t.Errorf("the output does not have %q:\n%s", w, out)
				}
			}
			for _, w := range tc.notWant {
				if strings.Contains(out, w) {
					t.Errorf("the output has %q:\n%s", w, out)
				}
			}
			if strings.ContainsAny(out, "\u2014\u2013") {
				t.Errorf("the output has a dash that is not a hyphen:\n%s", out)
			}
			if _, err := os.Stat(filepath.Join(root, "reports")); err == nil {
				t.Errorf("mmp agent cost wrote a report without -write")
			}
		})
	}
}

func TestAgentCostWrite(t *testing.T) {
	root := costTree(t)
	costRecords(t, root)
	var out, errb bytes.Buffer
	if code := run([]string{"agent", "cost", "-content", root, "-from", "2026-11-01", "-to", "2026-11-30", "-write"}, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	dst := filepath.Join(root, "reports", "cost-2026-11-30.md")
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != out.String() {
		t.Errorf("the file differs from the output:\n%s\n%s", data, out.String())
	}
	if !strings.Contains(errb.String(), "wrote "+dst) {
		t.Errorf("mmp does not name the file: %s", errb.String())
	}
}

func TestAgentCostErrors(t *testing.T) {
	root := costTree(t)
	for _, tc := range []struct {
		code int
		args []string
	}{
		{2, []string{"-from", "November"}},
		{2, []string{"-to", "2026-13-01"}},
		{2, []string{"-from", "2026-11-30", "-to", "2026-11-01"}},
		{2, []string{"x"}},
		{1, []string{"-content", filepath.Join(root, "nothing")}},
	} {
		mmp(t, tc.code, append([]string{"agent", "cost", "-content", root}, tc.args...)...)
	}
	writeFile(t, filepath.Join(root, "agent", "ledger.jsonl"), "not json\n")
	mmp(t, 1, "agent", "cost", "-content", root)
}
