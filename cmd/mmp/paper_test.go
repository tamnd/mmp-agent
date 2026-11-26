package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tamnd/mmp-agent/blueprint"
)

// paperTree writes a content repository with one node and one paper. The record of the paper says that its result is unstated, but the map says stated.
func paperTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "blueprint", "nodes", "MM00000001.yaml"), "tag: MM00000001\nkind: theorem\ntitle: t\nstatement: s\ntier: T3\nsources: [{paper: Ser67, loc: VI}]\nlean:\n  statement: MinimalModel.ToricConeTheorem\n")
	writeFile(t, filepath.Join(root, "records", "lean.json"), `{"commit":"`+goodCommit+`","decls":{"MinimalModel.ToricConeTheorem":{"kind":"def","axioms":[]}}}`)
	writeFile(t, filepath.Join(root, "papers", "Ser67.yaml"), "id: T0-04\nkey: Ser67\ntitle: Decomposition of toric morphisms\nauthors: [Miles Reid]\ndoi: null\narxiv: null\nread: {version: book, by: owner, date: 2026-11-25, tag: S}\ntier: T0\nwhy: the reference proof\nexpert_status: accepted\nresults:\n  - {id: \"Theorem 2\", statement: ToricConeTheorem, node: MM00000001, kind: theorem, status: unstated}\nlean_status: none\ncode_licence: null\n")
	return root
}

func TestPaperCommands(t *testing.T) {
	add := func(more ...string) []string {
		return append([]string{"add", "-key", "Kut80", "-id", "T5-10", "-title", "Toric Mori theory and Fano manifolds", "-tier", "T5", "-why", "the toric MMP with scaling", "-expert-status", "accepted"}, more...)
	}
	for _, tc := range []struct {
		name  string
		args  []string
		code  int
		out   string
		check func(t *testing.T, root string)
	}{
		{name: "no subcommand", code: 2},
		{name: "unknown subcommand", args: []string{"frob"}, code: 2},
		{name: "list", args: []string{"list"}, code: 0, out: "Ser67    T0-04  T0   accepted  none           1 node   Decomposition of toric morphisms"},
		{name: "list a tier with no paper", args: []string{"list", "-tier", "T5"}, code: 0, out: "0 papers"},
		{name: "list a bad tier", args: []string{"list", "-tier", "T12"}, code: 2},
		{name: "show", args: []string{"show", "Ser67"}, code: 0, out: "result Theorem 2: ToricConeTheorem (theorem), stated in MM00000001; the record says unstated, but the map says stated"},
		{name: "show an unknown key", args: []string{"show", "Nope"}, code: 1},
		{name: "show needs a key", args: []string{"show"}, code: 2},
		{name: "add", args: add("-author", "Osamu Fujino", "-year", "1980", "-also", "T0-10", "-result", "Main theorem|ToricConeTheorem|MM00000001|theorem"), code: 0, out: "wrote", check: func(t *testing.T, root string) {
			data, err := os.ReadFile(filepath.Join(root, "papers", "Kut80.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{"id: T5-10\nkey: Kut80\n", "authors: [Osamu Fujino]", "doi: null", "arxiv: null", "read: {version: null, by: null, date: null, tag: U}", "status: stated", "lean_status: none", "code_licence: null"} {
				if !strings.Contains(string(data), want) {
					t.Errorf("the record does not have %q:\n%s", want, data)
				}
			}
			p, err := blueprint.Load(root)
			if err != nil {
				t.Fatal(err)
			}
			if probs := p.Check(); len(probs) > 0 {
				t.Errorf("mmp check reports %v", probs)
			}
		}},
		{name: "add an arXiv id", args: add("2401.01234v2"), code: 0, check: func(t *testing.T, root string) {
			data, _ := os.ReadFile(filepath.Join(root, "papers", "Kut80.yaml"))
			if !strings.Contains(string(data), "arxiv: 2401.01234v2") || !strings.Contains(string(data), "version: v2") {
				t.Errorf("the record does not have the arXiv id and its version:\n%s", data)
			}
		}},
		{name: "add a DOI", args: add("10.2307/1971151"), code: 0, check: func(t *testing.T, root string) {
			data, _ := os.ReadFile(filepath.Join(root, "papers", "Kut80.yaml"))
			if !strings.Contains(string(data), "doi: 10.2307/1971151") {
				t.Errorf("the record does not have the DOI:\n%s", data)
			}
		}},
		{name: "add something that is not an id", args: add("Kutzko"), code: 2},
		{name: "add two ids", args: add("2401.01234", "10.2307/1971151"), code: 2},
		{name: "add an arXiv id twice", args: add("-arxiv", "2401.01234", "2401.01234"), code: 2},
		{name: "add with no title", args: []string{"add", "-key", "Kut80", "-id", "T5-10", "-tier", "T5", "-why", "w", "-expert-status", "accepted"}, code: 2},
		{name: "add a key that exists", args: []string{"add", "-key", "Ser67", "-id", "T0-05", "-title", "t", "-tier", "T0", "-why", "w", "-expert-status", "accepted"}, code: 1},
		{name: "add a corpus id that exists", args: []string{"add", "-key", "Kut80", "-id", "T0-04", "-title", "t", "-tier", "T5", "-why", "w", "-expert-status", "accepted"}, code: 1},
		{name: "add a bad expert status", args: []string{"add", "-key", "Kut80", "-id", "T5-10", "-title", "t", "-tier", "T5", "-why", "w", "-expert-status", "proved"}, code: 1},
		{name: "add a bad key", args: []string{"add", "-key", "Kut_80", "-id", "T5-10", "-title", "t", "-tier", "T5", "-why", "w", "-expert-status", "accepted"}, code: 1},
		{name: "add a result of an unknown node", args: add("-result", "Main theorem|ToricMMP|MM99999999|theorem"), code: 1},
		{name: "add a bad result", args: add("-result", "Main theorem"), code: 2},
		{name: "add a read tag with no reader", args: add("-read-tag", "S"), code: 2},
		{name: "add a read tag with a reader", args: add("-read-tag", "S", "-read-by", "reader", "-read-date", "2026-12-01", "-read-version", "journal"), code: 0, check: func(t *testing.T, root string) {
			data, _ := os.ReadFile(filepath.Join(root, "papers", "Kut80.yaml"))
			if !strings.Contains(string(data), "read: {version: journal, by: reader, date: \"2026-12-01\", tag: S}") {
				t.Errorf("the record does not have the reading:\n%s", data)
			}
		}},
	} {
		root := paperTree(t)
		args := append([]string{"paper"}, tc.args...)
		if len(tc.args) > 0 {
			args = append([]string{"paper", tc.args[0], "-content", root}, tc.args[1:]...)
		}
		var out, errb strings.Builder
		if got := run(args, &out, &errb); got != tc.code {
			t.Errorf("%s: exit %d, want %d (%s)", tc.name, got, tc.code, errb.String())
			continue
		}
		if !strings.Contains(out.String(), tc.out) {
			t.Errorf("%s: the output does not have %q:\n%s", tc.name, tc.out, out.String())
		}
		if tc.check != nil {
			tc.check(t, root)
		}
		if tc.code != 0 && strings.HasPrefix(tc.name, "add") {
			if _, err := os.Stat(filepath.Join(root, "papers", "Kut80.yaml")); err == nil {
				t.Errorf("%s: the command failed but wrote papers/Kut80.yaml", tc.name)
			}
		}
	}
}

func TestPaperAddDoesNotOverwrite(t *testing.T) {
	root := paperTree(t)
	// A file with the name of the key but another key in it. add must refuse and leave the file as it is.
	writeFile(t, filepath.Join(root, "papers", "Kut80.yaml"), "id: T5-10\nkey: Other\ntitle: t\nread: {version: v1, by: owner, date: 2026-11-25, tag: U}\ntier: T5\nwhy: w\nexpert_status: accepted\nlean_status: none\n")
	var out, errb strings.Builder
	code := run([]string{"paper", "add", "-content", root, "-key", "Kut80", "-id", "T5-11", "-title", "t", "-tier", "T5", "-why", "w", "-expert-status", "accepted"}, &out, &errb)
	if code != 1 {
		t.Fatalf("exit %d, want 1 (%s)", code, errb.String())
	}
	data, _ := os.ReadFile(filepath.Join(root, "papers", "Kut80.yaml"))
	if !strings.Contains(string(data), "key: Other") {
		t.Errorf("add replaced the file:\n%s", data)
	}
}
