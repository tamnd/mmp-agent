package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/tamnd/mmp-agent/blueprint"
	"github.com/tamnd/mmp-agent/site"
)

// The answers of a row of the trust report, spec 14.
const (
	answerYes    = "yes"
	answerNo     = "no"
	answerOpen   = "open"
	answerNotRun = "not run"
)

// trustReport is records/trust.json. The site reads the fields updated and checks. The other fields give the data behind the answers.
type trustReport struct {
	Updated  string            `json:"updated"`
	Checks   []site.TrustCheck `json:"checks"`
	Lean     *trustLean        `json:"lean,omitempty"`
	Replay   string            `json:"replay"` // the replay record that the report read, or "none"
	Theorems []trustTheorem    `json:"theorems"`
	Decls    []trustDecl       `json:"declarations"`
}

// trustLean is the build that the report is about, from records/lean.json.
type trustLean struct {
	Repository string `json:"repository"`
	Commit     string `json:"commit"`
	Toolchain  string `json:"toolchain"`
	Mathlib    string `json:"mathlib"`
	Built      string `json:"built"`
	Checkout   string `json:"checkout,omitempty"` // the commit of the checkout of mmp-lean, when mmp trust had -lean
}

// trustTheorem is a top theorem of spec 14.2 with its own rows.
type trustTheorem struct {
	Theorem   string            `json:"theorem"`
	Statement string            `json:"statement,omitempty"`
	Milestone string            `json:"milestone"`
	Node      string            `json:"node,omitempty"`
	Status    string            `json:"status"`
	Checks    []site.TrustCheck `json:"checks"`
}

// trustDecl is a declaration that a node names, with what the record of the build says about it.
type trustDecl struct {
	Name   string   `json:"name"`
	Node   string   `json:"node"`
	Role   string   `json:"role"` // statement, proof or negation
	Found  bool     `json:"found"`
	Sorry  bool     `json:"sorry,omitempty"`
	Axioms []string `json:"axioms,omitempty"`
	Other  []string `json:"other_axioms,omitempty"` // the axioms that are not standard, sorryAx excluded
}

// replayRecord is the result file of the kernel replay. The trust workflow of mmp-lean writes it. mmp trust only reads it.
type replayRecord struct {
	Commit     string            `json:"commit"` // the commit of mmp-lean that the checkers replayed
	Date       string            `json:"date"`
	Run        string            `json:"run,omitempty"` // the address of the workflow run
	Checkers   []replayChecker   `json:"checkers"`
	Comparator *replayComparison `json:"comparator,omitempty"`
}

// replayChecker is one checker of the replay, for example leanchecker --fresh, lean4lean or nanoda.
type replayChecker struct {
	Name        string `json:"name"`
	Independent bool   `json:"independent"` // a type checker that is not the kernel of Lean itself
	OK          bool   `json:"ok"`
	Note        string `json:"note,omitempty"`
}

// replayComparison is the result of lake comparator against MinimalModel/Statements, for row Q4.
type replayComparison struct {
	OK   bool   `json:"ok"`
	Note string `json:"note,omitempty"`
}

func runTrust(args []string, stdout, stderr io.Writer) error {
	fl := flag.NewFlagSet("mmp trust", flag.ContinueOnError)
	fl.SetOutput(stderr)
	content := fl.String("content", ".", "the root of the content repository")
	lean := fl.String("lean", "", "a checkout of mmp-lean, for row Q0 and the statement hashes of row Q4 (optional; mmp trust does not build it)")
	replay := fl.String("replay", "", "the result file of the kernel replay (default: records/replay.json of the content repository, when it exists)")
	quick := fl.Bool("quick", false, "do not read the result of the kernel replay; rows Q3 and Q4 are not run")
	date := fl.String("date", "", "the date of the report (default: today, UTC)")
	if err := parse(fl, args); err != nil {
		return err
	}
	if *date == "" {
		*date = time.Now().UTC().Format("2006-01-02")
	}
	if !dateRE.MatchString(*date) {
		return fmt.Errorf("%w: the date %q is not YYYY-MM-DD", errUsage, *date)
	}
	p, err := blueprint.Load(*content)
	if err != nil {
		return err
	}
	in := trustInput{project: p, date: *date, replayPath: "none"}
	if *lean != "" {
		if in.checkout, err = gitHead(*lean); err != nil {
			return err
		}
		if in.hashProblems, err = statementHashes(*lean); err != nil {
			return err
		}
		in.hashesRead = true
	}
	if !*quick {
		path, explicit := *replay, *replay != ""
		if !explicit {
			path = filepath.Join(*content, "records", "replay.json")
		}
		data, err := os.ReadFile(path) // #nosec G304 -- a record that the user names
		switch {
		case err == nil:
			var r replayRecord
			dec := json.NewDecoder(bytes.NewReader(data))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&r); err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			in.replay, in.replayPath = &r, path
		case explicit || !errors.Is(err, fs.ErrNotExist):
			return err
		}
	}
	in.quick = *quick
	rep := buildTrust(in)
	out, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return err
	}
	dst := filepath.Join(*content, "records", "trust.json")
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return err
	}
	if err := os.WriteFile(dst, append(out, '\n'), 0o644); err != nil { // #nosec G306 -- a public record
		return err
	}
	writeTrust(stdout, rep)
	fmt.Fprintf(stdout, "wrote %s\n", dst)
	no := 0
	for _, c := range rep.Checks {
		if c.State == answerNo {
			no++
		}
	}
	if no > 0 {
		return fmt.Errorf("the trust report has %s with the answer no", plural(no, "row"))
	}
	return nil
}

// trustInput is what buildTrust reads. The command fills it from the files, and the tests fill it directly.
type trustInput struct {
	project      *blueprint.Project
	date         string
	checkout     string   // the commit of the checkout of mmp-lean, or empty
	hashesRead   bool     // the statement hashes were checked
	hashProblems []string // the problems of manifests/statements.sha256
	replay       *replayRecord
	replayPath   string
	quick        bool
}

// buildTrust computes the rows of the trust report. A row that needs a person, a build or a kernel replay that mmp did not see is "not run" or "open", never "yes".
func buildTrust(in trustInput) trustReport {
	p := in.project
	rep := trustReport{Updated: in.date, Replay: in.replayPath}
	rows := site.DefaultTrust().Checks
	set := func(id, state, note string) {
		for i := range rows {
			if rows[i].ID == id {
				rows[i].State, rows[i].Note = state, note
			}
		}
	}
	rep.Decls = trustDecls(p)
	if p.Lean != nil {
		rep.Lean = &trustLean{Repository: p.Lean.Repository, Commit: p.Lean.Commit, Toolchain: p.Lean.Toolchain, Mathlib: p.Lean.Mathlib, Built: p.Lean.Built, Checkout: in.checkout}
	}

	// Q0: the commit of the record against the checkout.
	switch {
	case p.Lean == nil:
		set("Q0", answerNotRun, "There is no records/lean.json.")
	case in.checkout == "":
		set("Q0", answerNotRun, fmt.Sprintf("records/lean.json is of commit %.7s. Run mmp trust with -lean to compare it with a checkout of mmp-lean.", p.Lean.Commit))
	case in.checkout == p.Lean.Commit:
		set("Q0", answerYes, fmt.Sprintf("records/lean.json and the checkout of mmp-lean are both at commit %.7s.", p.Lean.Commit))
	default:
		set("Q0", answerNo, fmt.Sprintf("records/lean.json is of commit %.7s, but the checkout of mmp-lean is at commit %.7s.", p.Lean.Commit, in.checkout))
	}

	// Q1: the record of the build exists. mmp trust does not build.
	if p.Lean == nil || len(p.Lean.Decls) == 0 {
		set("Q1", answerNotRun, "There is no record of a build in records/lean.json.")
	} else {
		set("Q1", answerYes, fmt.Sprintf("records/lean.json is the record of the build of commit %.7s with Lean %s and Mathlib %s on %s. mmp trust reads the record and does not build.", p.Lean.Commit, p.Lean.Toolchain, p.Lean.Mathlib, p.Lean.Built))
	}

	// Q2: the axioms of the declarations that the nodes name.
	state, note := axiomRow(p, rep.Decls)
	set("Q2", state, note)

	// Q3 and Q4: the kernel replay and the comparator, from the replay record.
	state, note = replayRow(in)
	set("Q3", state, note)
	state, note = comparatorRow(in)
	set("Q4", state, note)

	// Q5: the fidelity battery.
	state, note = fidelityRow5(p)
	set("Q5", state, note)

	// Q6: the hypotheses of the proved-mod nodes.
	state, note = hypothesisRow(p)
	set("Q6", state, note)

	set("Q7", answerNotRun, "A person answers this row from the reviews of the handbook. mmp trust does not read the handbook yet.")
	set("Q8", answerNotRun, "A person answers this row with a link to an independent run. mmp trust does not check it.")

	// Q9: the audit of the external library (spec 04.3 and 14.5).
	state, note = auditRow(p)
	set("Q9", state, note)
	set("Q10", answerNotRun, "A person answers this row from the letter and the answer in each claim record. mmp trust does not read the letters.")
	rep.Checks = rows
	rep.Theorems = topTheorems(p, rep.Decls)
	return rep
}

// trustDecls returns the declarations that the nodes name, sorted by name and role.
func trustDecls(p *blueprint.Project) []trustDecl {
	var out []trustDecl
	for _, n := range p.Nodes {
		for _, nr := range []struct{ role, name string }{{"statement", n.Lean.Statement}, {"proof", n.Lean.Proof}, {"negation", n.Lean.Negation}} {
			if nr.name == "" {
				continue
			}
			td := trustDecl{Name: nr.name, Node: n.Tag, Role: nr.role}
			if p.Lean != nil {
				if d, ok := p.Lean.Decls[nr.name]; ok {
					td.Found, td.Sorry, td.Axioms = true, d.Sorry(), d.Axioms
					for _, a := range nonStandard(d) {
						if a != "sorryAx" {
							td.Other = append(td.Other, a)
						}
					}
				}
			}
			out = append(out, td)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		if out[i].Role != out[j].Role {
			return out[i].Role < out[j].Role
		}
		return out[i].Node < out[j].Node
	})
	return out
}

// hasProof reports whether the status of a node rests on a complete proof.
func hasProof(status string) bool {
	return status == blueprint.Proved || status == blueprint.ProvedMod || status == blueprint.Refuted
}

// axiomRow answers Q2. A declaration with an axiom that is not standard is a no wherever it is. A node whose status rests on a proof must also have its statement and its proof in the record, with no sorry.
func axiomRow(p *blueprint.Project, decls []trustDecl) (string, string) {
	if p.Lean == nil {
		return answerNotRun, "There is no records/lean.json."
	}
	var bad []string
	found, sorry := 0, 0
	for _, d := range decls {
		if d.Found {
			found++
		}
		if d.Sorry {
			sorry++
		}
		if len(d.Other) > 0 {
			bad = append(bad, d.Name+" uses "+strings.Join(d.Other, ", "))
		}
	}
	proofs := 0
	for _, n := range p.Nodes {
		if !hasProof(n.Status) {
			continue
		}
		proofs++
		for _, d := range decls {
			if d.Node != n.Tag || d.Role == "proof" && n.Status == blueprint.Refuted || d.Role == "negation" && n.Status != blueprint.Refuted {
				continue
			}
			switch {
			case !d.Found:
				bad = append(bad, n.Tag+" is "+n.Status+", but the record has no "+d.Name)
			case d.Sorry:
				bad = append(bad, n.Tag+" is "+n.Status+", but "+d.Name+" depends on sorry")
			}
		}
	}
	sort.Strings(bad)
	bad = dedupe(bad)
	if len(bad) > 0 {
		return answerNo, strings.Join(bad, ". ") + "."
	}
	if proofs == 0 {
		return answerNotRun, "No node has a complete proof yet."
	}
	note := fmt.Sprintf("The nodes name %d declarations, and records/lean.json has %d of them. Each of them uses only propext, Classical.choice and Quot.sound.", len(decls), found)
	if sorry > 0 {
		note += fmt.Sprintf(" Sorry is in %s of nodes that are not proved.", plural(sorry, "declaration"))
	}
	return answerYes, note + fmt.Sprintf(" The record has a complete proof for %s.", plural(proofs, "node"))
}

// replayRow answers Q3 from the replay record.
func replayRow(in trustInput) (string, string) {
	r := in.replay
	switch {
	case in.quick:
		return answerNotRun, "The kernel replay was not read: mmp trust ran with -quick."
	case r == nil:
		return answerNotRun, "The kernel replay was not run: there is no replay record records/replay.json."
	case in.project.Lean == nil:
		return answerNotRun, "There is no records/lean.json to compare with the replay record."
	case r.Commit != in.project.Lean.Commit:
		return answerNotRun, fmt.Sprintf("The replay record is of commit %.7s, but records/lean.json is of commit %.7s.", r.Commit, in.project.Lean.Commit)
	case len(r.Checkers) == 0:
		return answerNotRun, "The replay record has no checkers."
	}
	var failed, passed []string
	independent := false
	for _, c := range r.Checkers {
		if !c.OK {
			failed = append(failed, c.Name)
			continue
		}
		passed = append(passed, c.Name)
		independent = independent || c.Independent
	}
	switch {
	case len(failed) > 0:
		return answerNo, fmt.Sprintf("On %s the replay of commit %.7s failed in %s.", r.Date, r.Commit, strings.Join(failed, ", "))
	case !independent:
		return answerNotRun, fmt.Sprintf("On %s only the kernel of Lean replayed commit %.7s (%s). No independent checker ran.", r.Date, r.Commit, strings.Join(passed, ", "))
	}
	return answerYes, fmt.Sprintf("On %s %s accepted commit %.7s.", r.Date, strings.Join(passed, ", "), r.Commit)
}

// comparatorRow answers Q4 from the statement hashes and the comparator result of the replay record. Matching hashes alone are not a yes, because they do not show that each proof has the pinned statement.
func comparatorRow(in trustInput) (string, string) {
	if len(in.hashProblems) > 0 {
		return answerNo, strings.Join(in.hashProblems, ". ") + "."
	}
	hashes := ""
	if in.hashesRead {
		hashes = " The statement files match manifests/statements.sha256."
	}
	r := in.replay
	switch {
	case in.quick || r == nil || r.Comparator == nil:
		return answerNotRun, "lake comparator was not run." + hashes
	case in.project.Lean == nil || r.Commit != in.project.Lean.Commit:
		return answerNotRun, "The comparator result is not of the commit of records/lean.json." + hashes
	case !r.Comparator.OK:
		return answerNo, strings.TrimSpace(fmt.Sprintf("On %s lake comparator found a proof that does not match its pinned statement. %s", r.Date, r.Comparator.Note))
	}
	return answerYes, fmt.Sprintf("On %s lake comparator matched each proof of commit %.7s to its pinned statement.%s", r.Date, r.Commit, hashes)
}

// fidelityRow5 answers Q5 from records/fidelity.json. The battery needs people, so a row that is not complete is open.
func fidelityRow5(p *blueprint.Project) (string, string) {
	nodes := statementNodes(p)
	if len(nodes) == 0 {
		return answerNotRun, "The map has no Lean statements."
	}
	pass, entries := 0, 0
	for name := range nodes {
		f, ok := p.Fidelity[name]
		if !ok {
			continue
		}
		entries++
		if len(f.Missing(name)) == 0 {
			pass++
		}
	}
	note := fmt.Sprintf("%d of the %d Lean statements of the map pass the fidelity checks of their class (spec 06.3). %d have an entry in records/fidelity.json.", pass, len(nodes), entries)
	if pass == len(nodes) {
		return answerYes, note
	}
	return answerOpen, note
}

// hypothesisRow answers Q6: the hypotheses that the proved-mod nodes take, with the expert status of their papers.
func hypothesisRow(p *blueprint.Project) (string, string) {
	var parts []string
	for _, n := range p.Nodes {
		if n.Status != blueprint.ProvedMod {
			continue
		}
		if len(n.Hypotheses) == 0 {
			parts = append(parts, n.Tag+" uses a node that is not proved")
			continue
		}
		for _, h := range n.Hypotheses {
			s := n.Tag + " takes " + h
			if m := p.Node(h); m != nil {
				var papers []string
				for _, src := range m.Sources {
					if pp := p.Paper(src.Paper); pp != nil {
						papers = append(papers, pp.Key+" "+pp.ExpertStatus)
					}
				}
				if len(papers) > 0 {
					s += " (" + strings.Join(papers, ", ") + ")"
				}
			}
			parts = append(parts, s)
		}
	}
	if len(parts) == 0 {
		return answerYes, "No node is proved-mod, so no proof takes a published theorem as a hypothesis."
	}
	return answerOpen, strings.Join(parts, ". ") + ". A person checks the expert status of each source."
}

// topTheorems returns the rows of each top theorem of spec 14.2: the axioms of its proof and the fidelity of its statement.
func topTheorems(p *blueprint.Project, decls []trustDecl) []trustTheorem {
	var out []trustTheorem
	for _, t := range blueprint.TopTheorems {
		tt := trustTheorem{Theorem: t.Theorem, Statement: t.Statement, Milestone: t.Milestone, Status: "not in the map"}
		var node *blueprint.Node
		for _, n := range p.Nodes {
			if n.Lean.Proof == t.Theorem || node == nil && t.Statement != "" && n.Lean.Statement == t.Statement {
				node = n
			}
		}
		if node != nil {
			tt.Node, tt.Status = node.Tag, node.Status
		}
		q2 := site.TrustCheck{ID: "Q2", Question: "Which axioms does the theorem use?", State: answerNotRun, Note: "The record of the build has no proof " + t.Theorem + "."}
		for _, d := range decls {
			if d.Name != t.Theorem || d.Role != "proof" || !d.Found {
				continue
			}
			switch {
			case len(d.Other) > 0:
				q2.State, q2.Note = answerNo, "The proof uses "+strings.Join(d.Other, ", ")+"."
			case d.Sorry:
				q2.State, q2.Note = answerNotRun, "The proof depends on sorry."
			default:
				q2.State, q2.Note = answerYes, "The proof uses "+axiomList(d.Axioms)+"."
			}
		}
		q5 := site.TrustCheck{ID: "Q5", Question: "Does the statement mean what the docstring says?", State: answerNotRun, Note: "records/fidelity.json has no entry for the statement."}
		if t.Statement == "" {
			q5.Note = "Spec 06.2 does not fix the statement yet."
		} else if f, ok := p.Fidelity[t.Statement]; ok {
			if m := f.Missing(t.Statement); len(m) == 0 {
				q5.State, q5.Note = answerYes, "The statement passes the fidelity checks of the class "+f.Class+"."
			} else {
				q5.State, q5.Note = answerOpen, "The statement does not pass yet: "+strings.Join(m, "; ")+"."
			}
		}
		tt.Checks = []site.TrustCheck{q2, q5}
		out = append(out, tt)
	}
	return out
}

func axiomList(axioms []string) string {
	if len(axioms) == 0 {
		return "no axioms"
	}
	return strings.Join(axioms, ", ")
}

func dedupe(sorted []string) []string {
	var out []string
	for i, s := range sorted {
		if i == 0 || s != sorted[i-1] {
			out = append(out, s)
		}
	}
	return out
}

// writeTrust writes the trust report as text.
func writeTrust(w io.Writer, rep trustReport) {
	if l := rep.Lean; l != nil {
		fmt.Fprintf(w, "trust report of %s at commit %.7s (Lean %s, Mathlib %s, built %s), %s\n", l.Repository, l.Commit, l.Toolchain, l.Mathlib, l.Built, rep.Updated)
	} else {
		fmt.Fprintf(w, "trust report with no record of the build, %s\n", rep.Updated)
	}
	for _, c := range rep.Checks {
		fmt.Fprintf(w, "%s  %-7s  %s\n", c.ID, c.State, c.Note)
	}
	fmt.Fprintln(w, "top theorems:")
	for _, t := range rep.Theorems {
		node := t.Node
		if node == "" {
			node = "-"
		}
		fmt.Fprintf(w, "  %s  %s  %s %s  Q2 %s, Q5 %s\n", t.Theorem, t.Milestone, node, t.Status, t.Checks[0].State, t.Checks[1].State)
	}
}

// gitHead returns the commit of the checkout at dir. It only reads the repository.
func gitHead(dir string) (string, error) {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output() // #nosec G204 -- git with a directory that the user names
	if err != nil {
		return "", fmt.Errorf("%s: cannot read the commit of the checkout: %w", dir, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// statementHashes checks manifests/statements.sha256 of mmp-lean against the files of MinimalModel/Statements. It returns the problems: a file with another hash, a missing file, and a statement file that is not in the manifest.
func statementHashes(root string) ([]string, error) {
	f, err := os.Open(filepath.Join(root, "manifests", "statements.sha256")) // #nosec G304 -- a file of mmp-lean
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var probs []string
	listed := map[string]bool{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		sum, path, ok := strings.Cut(line, "  ")
		if !ok {
			probs = append(probs, "manifests/statements.sha256 has a bad line: "+line)
			continue
		}
		path = strings.TrimPrefix(path, "*")
		listed[path] = true
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path))) // #nosec G304 -- a file of mmp-lean
		if err != nil {
			probs = append(probs, path+" is in manifests/statements.sha256 but cannot be read")
			continue
		}
		h := sha256.Sum256(data)
		if hex.EncodeToString(h[:]) != sum {
			probs = append(probs, path+" does not have the hash of manifests/statements.sha256")
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	files, err := leanFiles(filepath.Join(root, "MinimalModel", "Statements"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	for _, file := range files {
		if r := relPath(root, file); !listed[r] {
			probs = append(probs, r+" is not in manifests/statements.sha256")
		}
	}
	sort.Strings(probs)
	return probs, nil
}

// auditRow is the row Q9. It reads records/audit.json. The answer is yes only when all the steps A1 to A8 are done and the audit has no open finding of severity 1 or 2.
func auditRow(p *blueprint.Project) (string, string) {
	a := p.Audit
	if a == nil {
		return answerNotRun, "There is no records/audit.json. The audit of AxiomMath/MMP has not started."
	}
	var done, left []string
	for _, step := range blueprint.AuditSteps {
		if a.Steps[step] == "done" {
			done = append(done, step)
		} else {
			left = append(left, step)
		}
	}
	serious := a.Findings["1"] + a.Findings["2"]
	switch {
	case serious > 0:
		return answerNo, fmt.Sprintf("The audit of %s at commit %.10s has %s of severity 1 or 2.", a.Source, a.Commit, plural(serious, "open finding"))
	case len(left) == 0 && a.Passed:
		return answerYes, fmt.Sprintf("The audit of %s at commit %.10s passed: the steps A1 to A8 are done, with no open finding of severity 1 or 2.", a.Source, a.Commit)
	case len(done) == 0:
		return answerOpen, fmt.Sprintf("The audit of %s at commit %.10s has no step done.", a.Source, a.Commit)
	default:
		return answerOpen, fmt.Sprintf("The audit of %s at commit %.10s has the steps %s done, and the steps %s open.", a.Source, a.Commit, strings.Join(done, ", "), strings.Join(left, ", "))
	}
}
