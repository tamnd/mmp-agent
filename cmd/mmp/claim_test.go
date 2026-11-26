package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// claimTree writes a content repository with a defined node, a stated node and a node of kind claim.
func claimTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	node := func(tag, kind, stmt string) {
		writeFile(t, filepath.Join(root, "blueprint", "nodes", tag+".yaml"), "tag: "+tag+"\nkind: "+kind+"\ntitle: t\nstatement: s\ntier: T3\nlean:\n  statement: "+stmt+"\n")
	}
	node("MM00000001", "definition", "MinimalModel.WeilGroup")
	node("MM00000002", "theorem", "MinimalModel.LocalExistence")
	node("MM00000003", "claim", "MinimalModel.Claim.FK")
	writeFile(t, filepath.Join(root, "records", "lean.json"), `{"commit":"`+goodCommit+`","decls":{
"MinimalModel.WeilGroup":{"kind":"def","axioms":[]},
"MinimalModel.LocalExistence":{"kind":"def","axioms":[]},
"MinimalModel.Claim.FK":{"kind":"def","axioms":[]},
"MinimalModel.Claim.FK.weil":{"kind":"theorem","axioms":["propext"]}}}`)
	return root
}

const goodClaim = `key: arXiv-2611-31396
title: Supercuspidals and double covers
authors: [Jessica Fintzen, Tasho Kaletha]
source: {arxiv: "2611.31396", url: null, version: v1}
date: "2026-11-25"
posted: "2026-11-20"
status: planned
route: R1
claims: a construction of supercuspidals through double covers
node: MM00000003
parts:
  - id: weil
    title: The twisted Heisenberg-Weil representation
    where: Section 3, Theorem 3.4
    statement: The character of the twisted Heisenberg-Weil representation over a finite field of odd characteristic.
    lean: MinimalModel.Claim.FK.weil
    uses: [MM00000001]
    cost: 2
    status: done
    result: The Lean proof builds with the standard axioms only.
  - id: types
    title: The types
    where: Section 5
    statement: The types are the types of the cover.
    uses: [weil, MM00000002]
    cost: 4
    status: proposed
letters:
  - {to: the authors, date: "2026-11-26", about: the part weil}
`

func TestClaimSource(t *testing.T) {
	for _, tc := range []struct {
		arg, arxiv, version, address, key string
	}{
		{"2611.31396", "2611.31396", "", "", "arXiv-2611-31396"},
		{"arXiv:2611.31396v2", "2611.31396", "v2", "", "arXiv-2611-31396"},
		{"https://arxiv.org/abs/2611.31396v2", "2611.31396", "v2", "", "arXiv-2611-31396"},
		{"https://www.arxiv.org/pdf/2611.31396v3.pdf", "2611.31396", "v3", "", "arXiv-2611-31396"},
		{"HTTPS://Example.org/paper/", "", "", "https://example.org/paper", ""},
	} {
		arxiv, version, address, err := claimSourceOf(tc.arg)
		if err != nil {
			t.Errorf("%s: %v", tc.arg, err)
			continue
		}
		if arxiv != tc.arxiv || version != tc.version || address != tc.address {
			t.Errorf("%s: got %q %q %q", tc.arg, arxiv, version, address)
		}
		if key := claimKey(arxiv, address); tc.key != "" && key != tc.key {
			t.Errorf("%s: the key is %s, want %s", tc.arg, key, tc.key)
		}
		if key := claimKey(arxiv, address); tc.key == "" && (!strings.HasPrefix(key, "url-") || len(key) != 14) {
			t.Errorf("%s: the key %s is not url- and 10 hex digits", tc.arg, key)
		}
	}
	for _, bad := range []string{"Fintzen", "ftp://example.org/x", "https://", ""} {
		if _, _, _, err := claimSourceOf(bad); err == nil {
			t.Errorf("%q: no error", bad)
		}
	}
}

func TestClaimCommands(t *testing.T) {
	for _, tc := range []struct {
		name   string
		args   []string
		record string // a claim record to write before the command
		code   int
		out    string
		check  func(t *testing.T, root string)
	}{
		{name: "no subcommand", code: 2},
		{name: "unknown subcommand", args: []string{"frob"}, code: 2},
		{name: "add", args: []string{"add", "-date", "2026-11-25", "-title", "Supercuspidals", "-author", "Jessica Fintzen", "https://arxiv.org/abs/2611.31396v1"}, code: 0, out: "mmp did not fetch the preprint", check: func(t *testing.T, root string) {
			data, err := os.ReadFile(filepath.Join(root, "claims", "arXiv-2611-31396.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"key: arXiv-2611-31396\n", "title: Supercuspidals\n", "authors: [Jessica Fintzen]", `source: {arxiv: "2611.31396", url: null, version: v1}`, `date: "2026-11-25"`, "status: fresh", "route: R1", "parts: []", "letters: []"} {
				if !strings.Contains(string(data), want) {
					t.Errorf("the record does not have %q:\n%s", want, data)
				}
			}
			var out, errb bytes.Buffer
			if code := run([]string{"check", "-content", root}, &out, &errb); code != 0 || !strings.Contains(out.String(), "1 claim") {
				t.Errorf("mmp check: exit %d: %s%s", code, out.String(), errb.String())
			}
		}},
		{name: "add an address", args: []string{"add", "https://example.org/fk.pdf"}, code: 0, out: "claims/url-", check: func(t *testing.T, root string) {
			files, _ := filepath.Glob(filepath.Join(root, "claims", "url-*.yaml"))
			if len(files) != 1 {
				t.Fatalf("got the files %v", files)
			}
			data, _ := os.ReadFile(files[0])
			if !strings.Contains(string(data), "url: 'https://example.org/fk.pdf'") {
				t.Errorf("the record does not have the address:\n%s", data)
			}
		}},
		{name: "add with a key", args: []string{"add", "-key", "FK26", "2611.31396"}, code: 0, out: "claims/FK26.yaml"},
		{name: "add a bad key", args: []string{"add", "-key", "FK_26", "2611.31396"}, code: 1},
		{name: "add a bad date", args: []string{"add", "-date", "25 November", "2611.31396"}, code: 2},
		{name: "add no source", args: []string{"add"}, code: 2},
		{name: "add something that is not a source", args: []string{"add", "Fintzen"}, code: 2},
		{name: "add a key that exists", args: []string{"add", "2611.31396v2"}, record: goodClaim, code: 1, out: ""},
		{name: "add a source that exists with another key", args: []string{"add", "-key", "FK26", "arXiv:2611.31396"}, record: goodClaim, code: 1},
		{name: "list", args: []string{"list"}, record: goodClaim, code: 0, out: "arXiv-2611-31396   planned   2026-11-25  2 parts  arXiv:2611.31396       Supercuspidals and double covers\n1 claim\n"},
		{name: "list a status with no claim", args: []string{"list", "-status", "done"}, record: goodClaim, code: 0, out: "0 claims"},
		{name: "list a bad status", args: []string{"list", "-status", "proved"}, code: 2},
		{name: "show", args: []string{"show", "arXiv-2611-31396"}, record: goodClaim, code: 0, out: "Supercuspidals and double covers"},
		{name: "show an unknown key", args: []string{"show", "Nope"}, code: 1},
		{name: "show needs a key", args: []string{"show"}, code: 2},
		{name: "plan", args: []string{"plan", "arXiv-2611-31396"}, record: goodClaim, code: 0, out: "2 parts with a total cost of 6 (dropped parts excluded); 1 self-contained, 1 done, 0 gaps", check: nil},
		{name: "plan marks a part that is not self-contained", args: []string{"plan", "arXiv-2611-31396"}, record: goodClaim, code: 0, out: "not self-contained: it uses MM00000002 (stated)"},
		{name: "plan of a fresh record", args: []string{"plan", "arXiv-2611-31396"}, record: "key: arXiv-2611-31396\ntitle: null\nauthors: []\nsource: {arxiv: \"2611.31396\", url: null, version: null}\ndate: \"2026-11-25\"\nposted: null\nstatus: fresh\nroute: R1\nclaims: null\nnode: null\nparts: []\nletters: []\n", code: 0, out: "The record has no parts yet"},
		{name: "plan a bad record", args: []string{"plan", "arXiv-2611-31396"}, record: strings.Replace(strings.Replace(goodClaim, "cost: 4", "cost: 9", 1), "route: R1", "route: R2", 1), code: 1, out: "the cost is 1 to 5"},
		{name: "plan a part with a use that is not there", args: []string{"plan", "arXiv-2611-31396"}, record: strings.Replace(goodClaim, "[weil, MM00000002]", "[weyl]", 1), code: 1, out: "not a part of the record and not a node"},
		{name: "plan a done part with no result", args: []string{"plan", "arXiv-2611-31396"}, record: strings.Replace(goodClaim, "    result: The Lean proof builds with the standard axioms only.\n", "", 1), code: 1, out: "has the status done but no result"},
		{name: "plan a node that is not a claim", args: []string{"plan", "arXiv-2611-31396"}, record: strings.Replace(goodClaim, "node: MM00000003", "node: MM00000002", 1), code: 1, out: "the node of a claim has the kind claim"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := claimTree(t)
			if tc.record != "" {
				writeFile(t, filepath.Join(root, "claims", "arXiv-2611-31396.yaml"), tc.record)
			}
			args := append([]string{"claim"}, tc.args...)
			if len(tc.args) > 0 {
				args = append([]string{"claim", tc.args[0], "-content", root}, tc.args[1:]...)
			}
			var out, errb bytes.Buffer
			if code := run(args, &out, &errb); code != tc.code {
				t.Fatalf("exit %d, want %d: %s%s", code, tc.code, out.String(), errb.String())
			}
			if !strings.Contains(out.String(), tc.out) {
				t.Errorf("the output does not have %q:\n%s", tc.out, out.String())
			}
			if tc.check != nil {
				tc.check(t, root)
			}
		})
	}
}

func TestCheckReadsClaims(t *testing.T) {
	root := claimTree(t)
	writeFile(t, filepath.Join(root, "claims", "arXiv-2611-31396.yaml"), goodClaim)
	writeFile(t, filepath.Join(root, "claims", "FK26.yaml"), strings.Replace(goodClaim, "key: arXiv-2611-31396", "key: FK26", 1))
	var out, errb bytes.Buffer
	if code := run([]string{"check", "-content", root}, &out, &errb); code != 1 {
		t.Fatalf("exit %d, want 1: %s%s", code, out.String(), errb.String())
	}
	if !strings.Contains(out.String(), "arXiv:2611.31396") {
		t.Errorf("mmp check does not report the source that two records share:\n%s", out.String())
	}
}

func TestClaimAddDoesNotOverwrite(t *testing.T) {
	root := claimTree(t)
	path := filepath.Join(root, "claims", "arXiv-2611-31396.yaml")
	writeFile(t, path, goodClaim)
	var out, errb bytes.Buffer
	if code := run([]string{"claim", "add", "-content", root, "2611.31396"}, &out, &errb); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	data, _ := os.ReadFile(path)
	if string(data) != goodClaim {
		t.Errorf("claim add changed the record:\n%s", data)
	}
}
