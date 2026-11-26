package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/tamnd/mmp-agent/blueprint"
)

func runFidelity(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("%w: fidelity needs a subcommand: list, show, set or run", errUsage)
	}
	switch args[0] {
	case "list":
		return fidelityList(args[1:], stdout, stderr)
	case "show":
		return fidelityShow(args[1:], stdout, stderr)
	case "set":
		return fidelitySet(args[1:], stdout, stderr)
	case "run":
		return fidelityRun(args[1:], stdout, stderr)
	}
	return fmt.Errorf("%w: unknown fidelity subcommand %q; use list, show, set or run", errUsage, args[0])
}

// fidelityRow is one statement of the map with its entry of records/fidelity.json.
type fidelityRow struct {
	Name    string
	Nodes   []string // the nodes that name the statement
	Entry   blueprint.Fidelity
	Found   bool   // records/fidelity.json has an entry
	F1      bool   // F1 as computed from records/lean.json
	F1Why   string // the reason for F1
	Missing []string
}

// statementNodes returns the Lean statements of the map and the nodes that name each one. A definition is not a statement of spec 06.2, so it is not in the list.
func statementNodes(p *blueprint.Project) map[string][]string {
	out := map[string][]string{}
	for _, n := range p.Nodes {
		if n.Lean.Statement != "" && n.Kind != "definition" {
			out[n.Lean.Statement] = append(out[n.Lean.Statement], n.Tag)
		}
	}
	return out
}

// checkF1 computes check F1 of spec 06.3 from records/lean.json: the declaration is in the record of the build, it has no sorry, and it uses the standard axioms only.
func checkF1(p *blueprint.Project, name string) (bool, string) {
	if p.Lean == nil {
		return false, "there is no records/lean.json"
	}
	d, ok := p.Lean.Decls[name]
	switch {
	case !ok:
		return false, "records/lean.json has no declaration " + name
	case d.Sorry():
		return false, "the declaration depends on sorry"
	case !d.Clean():
		return false, "the declaration uses an axiom that is not standard: " + strings.Join(nonStandard(d), ", ")
	}
	return true, fmt.Sprintf("the declaration builds at commit %.7s with the standard axioms only", p.Lean.Commit)
}

// nonStandard returns the axioms of the declaration that are not standard, sorryAx included.
func nonStandard(d blueprint.Decl) []string {
	var out []string
	for _, a := range d.Axioms {
		if !blueprint.StandardAxioms[a] {
			out = append(out, a)
		}
	}
	return out
}

// defaultClass is the class of a new entry, from the kind of the node.
func defaultClass(kind string) string {
	switch kind {
	case "conjecture":
		return "open"
	case "claim":
		return "claimed"
	}
	return "theorem"
}

// fidelityRows returns a row for each statement of the map and for each entry of records/fidelity.json, sorted by name.
func fidelityRows(p *blueprint.Project) []fidelityRow {
	nodes := statementNodes(p)
	seen := map[string]bool{}
	var names []string
	for name := range nodes {
		names = append(names, name)
		seen[name] = true
	}
	for name := range p.Fidelity {
		if !seen[name] {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	var out []fidelityRow
	for _, name := range names {
		out = append(out, fidelityRowOf(p, name, nodes[name]))
	}
	return out
}

func fidelityRowOf(p *blueprint.Project, name string, nodes []string) fidelityRow {
	r := fidelityRow{Name: name, Nodes: nodes}
	r.Entry, r.Found = p.Fidelity[name]
	r.F1, r.F1Why = checkF1(p, name)
	if r.Found {
		r.Missing = r.Entry.Missing(name)
	}
	return r
}

// checksText is the checks of an entry in one line, for example "F1 yes, F2 yes, F3 no".
func checksText(f blueprint.Fidelity, name string) string {
	req := blueprint.RequiredChecks(f.Class, blueprint.IsTopStatement(name))
	var parts []string
	for _, c := range blueprint.FidelityChecks {
		v, ok := f.Checks[c]
		if !ok && !contains(req, c) {
			continue
		}
		word := "no"
		if v {
			word = "yes"
		}
		parts = append(parts, c+" "+word)
	}
	return strings.Join(parts, ", ")
}

func fidelityList(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("mmp fidelity list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	content := fs.String("content", ".", "the root of the content repository")
	if err := parse(fs, args); err != nil {
		return err
	}
	p, err := blueprint.Load(*content)
	if err != nil {
		return err
	}
	rows := fidelityRows(p)
	pass, none := 0, 0
	for _, r := range rows {
		switch {
		case !r.Found:
			none++
			fmt.Fprintf(stdout, "%s  no entry  (F1 from the record: %s)\n", r.Name, yesNo(r.F1))
		default:
			state := "pass"
			if len(r.Missing) > 0 {
				state = "not passed"
			} else {
				pass++
			}
			note := ""
			if r.Entry.Pass != (len(r.Missing) == 0) {
				note = fmt.Sprintf("  (the record says pass %t)", r.Entry.Pass)
			}
			if r.Entry.Checks["F1"] != r.F1 {
				note += fmt.Sprintf("  (F1 from the record of the build is %s)", yesNo(r.F1))
			}
			fmt.Fprintf(stdout, "%s  %s  %s  %s%s\n", r.Name, r.Entry.Class, state, checksText(r.Entry, r.Name), note)
		}
	}
	fmt.Fprintf(stdout, "%d statements in the map or in records/fidelity.json: %d pass, %d have no entry\n", len(rows), pass, none)
	return nil
}

// resolveStatement returns the Lean statement that arg names: a node tag or a Lean name. The statement must be the statement of a node of the map.
func resolveStatement(p *blueprint.Project, arg string) (string, []string, error) {
	nodes := statementNodes(p)
	if n := p.Node(arg); n != nil {
		if n.Lean.Statement == "" {
			return "", nil, fmt.Errorf("the node %s has no Lean statement", arg)
		}
		if n.Kind == "definition" {
			return "", nil, fmt.Errorf("the node %s is a definition; the fidelity checks are for statements", arg)
		}
		return n.Lean.Statement, nodes[n.Lean.Statement], nil
	}
	if ns, ok := nodes[arg]; ok {
		return arg, ns, nil
	}
	return "", nil, fmt.Errorf("no node of the map has the tag or the Lean statement %q", arg)
}

func fidelityShow(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("mmp fidelity show", flag.ContinueOnError)
	fs.SetOutput(stderr)
	content := fs.String("content", ".", "the root of the content repository")
	if err := fs.Parse(args); err != nil {
		return usageErr(err)
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("%w: fidelity show needs one Lean name or node tag", errUsage)
	}
	p, err := blueprint.Load(*content)
	if err != nil {
		return err
	}
	name, nodes, err := resolveStatement(p, fs.Arg(0))
	if err != nil {
		return err
	}
	writeFidelity(stdout, fidelityRowOf(p, name, nodes))
	return nil
}

// writeFidelity writes the entry of one statement as text.
func writeFidelity(w io.Writer, r fidelityRow) {
	fmt.Fprintf(w, "statement: %s\n", r.Name)
	fmt.Fprintf(w, "nodes: %s\n", strings.Join(r.Nodes, ", "))
	if blueprint.IsTopStatement(r.Name) {
		fmt.Fprintln(w, "top theorem: yes, a theorem of spec 14.2 proves it, so a theorem also needs F6")
	}
	fmt.Fprintf(w, "F1 from records/lean.json: %s, %s\n", yesNo(r.F1), r.F1Why)
	if !r.Found {
		fmt.Fprintln(w, "entry: none in records/fidelity.json")
		return
	}
	fmt.Fprintf(w, "class: %s\n", r.Entry.Class)
	fmt.Fprintf(w, "needs: %s\n", strings.Join(blueprint.RequiredChecks(r.Entry.Class, blueprint.IsTopStatement(r.Name)), ", "))
	fmt.Fprintf(w, "checks: %s\n", checksText(r.Entry, r.Name))
	for _, rv := range r.Entry.Reviewers {
		fmt.Fprintf(w, "reviewer: %s, %s, %s\n", rv.Name, rv.Group, rv.Date)
	}
	if len(r.Missing) == 0 {
		fmt.Fprintln(w, "pass: yes")
	} else {
		fmt.Fprintf(w, "pass: no, because %s\n", strings.Join(r.Missing, "; "))
	}
}

func fidelitySet(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("mmp fidelity set", flag.ContinueOnError)
	fs.SetOutput(stderr)
	content := fs.String("content", ".", "the root of the content repository")
	class := fs.String("class", "", "the class of the statement: "+strings.Join(blueprint.FidelityClasses, ", ")+" (default: from the kind of the node)")
	var reviewers multiFlag
	fs.Var(&reviewers, "reviewer", "a reviewer of F6 as \"name|group|YYYY-MM-DD\"; give -reviewer again for more than one")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: mmp fidelity set [flags] <Lean name | node tag> [F2=true ...]")
		fmt.Fprintln(stderr, "F1 is computed from records/lean.json. A person sets F2 to F7.")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return usageErr(err)
	}
	if fs.NArg() < 1 {
		return fmt.Errorf("%w: fidelity set needs a Lean name or a node tag", errUsage)
	}
	if *class != "" && !contains(blueprint.FidelityClasses, *class) {
		return fmt.Errorf("%w: the class %q is not one of %s", errUsage, *class, strings.Join(blueprint.FidelityClasses, ", "))
	}
	sets := map[string]bool{}
	for _, kv := range fs.Args()[1:] {
		k, v, ok := strings.Cut(kv, "=")
		b, err := strconv.ParseBool(v)
		switch {
		case !ok || err != nil:
			return fmt.Errorf("%w: %q is not CHECK=true or CHECK=false", errUsage, kv)
		case k == "F1":
			return fmt.Errorf("%w: F1 is computed from records/lean.json; run mmp fidelity run", errUsage)
		case !contains(blueprint.FidelityChecks, k):
			return fmt.Errorf("%w: the check %q is not one of F2 to F7", errUsage, k)
		}
		sets[k] = b
	}
	var revs []blueprint.Reviewer
	for _, s := range reviewers {
		parts := strings.Split(s, "|")
		if len(parts) != 3 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" || !dateRE.MatchString(strings.TrimSpace(parts[2])) {
			return fmt.Errorf("%w: the reviewer %q is not \"name|group|YYYY-MM-DD\"", errUsage, s)
		}
		revs = append(revs, blueprint.Reviewer{Name: strings.TrimSpace(parts[0]), Group: strings.TrimSpace(parts[1]), Date: strings.TrimSpace(parts[2])})
	}
	p, err := blueprint.Load(*content)
	if err != nil {
		return err
	}
	if p.Lean == nil {
		return errors.New("there is no records/lean.json, so F1 cannot be computed; run mmp index first")
	}
	name, nodes, err := resolveStatement(p, fs.Arg(0))
	if err != nil {
		return err
	}
	f, ok := p.Fidelity[name]
	if !ok {
		f = blueprint.Fidelity{Class: defaultClass(p.Node(nodes[0]).Kind)}
	}
	if *class != "" {
		f.Class = *class
	}
	f.Checks = copyChecks(f.Checks)
	for k, v := range sets {
		f.Checks[k] = v
	}
	for _, rv := range revs {
		if !hasReviewer(f.Reviewers, rv.Name) {
			f.Reviewers = append(f.Reviewers, rv)
		}
	}
	if f.Checks["F6"] && len(f.Reviewers) < 2 {
		return errors.New("F6 needs the names of two reviewers; give them with -reviewer")
	}
	if p.Fidelity == nil {
		p.Fidelity = map[string]blueprint.Fidelity{}
	}
	p.Fidelity[name] = settle(p, name, f)
	if err := writeFidelityRecord(*content, p.Fidelity); err != nil {
		return err
	}
	writeFidelity(stdout, fidelityRowOf(p, name, nodes))
	return nil
}

// settle computes F1 from records/lean.json, adds each check that the class needs and that the entry does not have yet as false, and computes pass.
func settle(p *blueprint.Project, name string, f blueprint.Fidelity) blueprint.Fidelity {
	f.Checks = copyChecks(f.Checks)
	f.Checks["F1"], _ = checkF1(p, name)
	for _, c := range blueprint.RequiredChecks(f.Class, blueprint.IsTopStatement(name)) {
		if _, ok := f.Checks[c]; !ok {
			f.Checks[c] = false
		}
	}
	f.Pass = len(f.Missing(name)) == 0
	return f
}

func copyChecks(m map[string]bool) map[string]bool {
	out := make(map[string]bool, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func fidelityRun(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("mmp fidelity run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	content := fs.String("content", ".", "the root of the content repository")
	node := fs.String("node", "", "the tag of the node whose statement to check")
	all := fs.Bool("all", false, "check the statement of each node of the map")
	if err := parse(fs, args); err != nil {
		return err
	}
	if (*node == "") == !*all {
		return fmt.Errorf("%w: fidelity run needs -node or -all", errUsage)
	}
	p, err := blueprint.Load(*content)
	if err != nil {
		return err
	}
	if p.Lean == nil {
		return errors.New("there is no records/lean.json, so F1 cannot be computed; run mmp index first")
	}
	var names []string
	if *all {
		for name := range statementNodes(p) {
			names = append(names, name)
		}
		sort.Strings(names)
	} else {
		name, _, err := resolveStatement(p, *node)
		if err != nil {
			return err
		}
		names = []string{name}
	}
	if p.Fidelity == nil {
		p.Fidelity = map[string]blueprint.Fidelity{}
	}
	nodes := statementNodes(p)
	for _, name := range names {
		f, ok := p.Fidelity[name]
		if !ok {
			f = blueprint.Fidelity{Class: defaultClass(p.Node(nodes[name][0]).Kind)}
		}
		f = settle(p, name, f)
		p.Fidelity[name] = f
		_, why := checkF1(p, name)
		fmt.Fprintf(stdout, "%s  F1 %s, %s; pass %s\n", name, yesNo(f.Checks["F1"]), why, yesNo(f.Pass))
	}
	if err := writeFidelityRecord(*content, p.Fidelity); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "wrote %s: checked %s\n", filepath.Join(*content, "records", "fidelity.json"), plural(len(names), "statement"))
	return nil
}

// writeFidelityRecord writes records/fidelity.json, with the names in sorted order.
func writeFidelityRecord(content string, m map[string]blueprint.Fidelity) error {
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	dst := filepath.Join(content, "records", "fidelity.json")
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return err
	}
	return os.WriteFile(dst, append(out, '\n'), 0o644) // #nosec G306 -- a public record
}

func hasReviewer(list []blueprint.Reviewer, name string) bool {
	for _, r := range list {
		if r.Name == name {
			return true
		}
	}
	return false
}

// plural is the number n and the word, with an s when n is not 1.
func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// usageErr makes a usage error of a bad flag, and keeps flag.ErrHelp.
func usageErr(err error) error {
	if errors.Is(err, flag.ErrHelp) {
		return err
	}
	return fmt.Errorf("%w: %v", errUsage, err)
}

// multiFlag is a flag that can be given more than once.
type multiFlag []string

func (m *multiFlag) String() string { return strings.Join(*m, ",") }

func (m *multiFlag) Set(s string) error {
	*m = append(*m, s)
	return nil
}
