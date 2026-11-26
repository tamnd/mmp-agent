package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/tamnd/mmp-agent/blueprint"
	"go.yaml.in/yaml/v3"
)

// agentRole is a row of the role table of spec 10.3. Target is what the queue of the role holds: "node", "claim", or "" for a role whose input is not a node or a claim record, so that it has no queue yet.
type agentRole struct {
	Name    string
	Input   string
	Output  string
	Checked string
	Target  string
}

// agentRoles is the role table of spec 10.3.
var agentRoles = []agentRole{
	{"reader", "a paper or claim record and its text", "the list of its numbered results with page and number; proposed nodes and uses edges; for a claim, the self-contained parts and their dependencies", "a person accepts each node", "claim"},
	{"stater", "a node", "the Lean statement in Statements/ or in a module, with a docstring", "build; F1, F2 of spec 06.3", "node"},
	{"prover", "a stated node whose uses are proved or proved-mod", "a proof, or a decomposition into new lemma nodes with statements", "build; mmp index shows no sorry", "node"},
	{"planner", "a route and its nodes", "the next nodes to state; a decomposition of a hard node into steps; a short note on why", "a person, for any change of routes.yaml or targets.yaml", ""},
	{"refuter", "a statement, a conjecture or a step of a fresh claim", "a Lean proof of the negation, a counterexample from the lab, or a trivial proof (F3, F4)", "build; lab record", "node"},
	{"explorer", "a question of a route", "a lab spec and its run; a conjecture node when the data suggests one", "lab record; a person for a new conjecture node", "node"},
	{"porter", "a file of a ported source, or a failing build after a bump", "the file at the project pin, with its provenance line; the bridge to the project statement", "build; mmp port check", ""},
	{"auditor", "an AxiomMath/MMP definition or theorem and its source reference (KM98, BCHM, a Stacks tag)", "a draft reading report for A5 and A6: the Lean definition, the textbook definition, the differences, a positive instance to try, a trivial proof to try", "two people sign each report (spec 04.3)", ""},
	{"explainer", "a declaration or a node", "a docstring, the L2 text of a node, the round-trip English of F5", "a person", "node"},
	{"writer", "a handbook chapter brief", "a chapter section, with examples and exercises (spec 11.3)", "a person; the handbook check", ""},
	{"linker", "a node", "its Math 2.0 node, its Stacks tags, its database objects (spec 13.2) and its references", "mmp site check", "node"},
}

func findRole(name string) *agentRole {
	for i := range agentRoles {
		if agentRoles[i].Name == name {
			return &agentRoles[i]
		}
	}
	return nil
}

func roleNames() string {
	var out []string
	for _, r := range agentRoles {
		out = append(out, r.Name)
	}
	return strings.Join(out, ", ")
}

// queueRole returns the role of a name, or a usage error for an unknown role or a role that has no queue yet.
func queueRole(name string) (*agentRole, error) {
	r := findRole(name)
	if r == nil {
		return nil, fmt.Errorf("%w: unknown role %q; use one of %s", errUsage, name, roleNames())
	}
	if r.Target == "" {
		return nil, fmt.Errorf("%w: the input of the role %s is %s, not a node or a claim record, so it has no queue yet", errUsage, r.Name, r.Input)
	}
	return r, nil
}

// done reports whether a node is finished for the nodes that use it.
func done(n *blueprint.Node) bool {
	return n.Status == blueprint.Proved || n.Status == blueprint.ProvedMod || n.Status == blueprint.Defined
}

// canAct reports whether the role can act on the node, with the reason.
func canAct(p *blueprint.Project, role string, n *blueprint.Node) (bool, string) {
	switch role {
	case "stater":
		switch {
		case n.Kind == "claim" && n.Lean.Statement == "":
			return true, "a fresh claim with no Lean statement"
		case n.Status == blueprint.Planned:
			return true, "the node is unstated: " + n.Why
		}
		return false, "the node is " + n.Status + ", and the stater role acts on an unstated node"
	case "prover":
		if n.Status != blueprint.Stated && n.Status != blueprint.Faithful {
			return false, "the node is " + n.Status + ", and the prover role acts on a stated or faithful node"
		}
		var wait []string
		for _, u := range n.Uses {
			if m := p.Node(u); m == nil || !done(m) {
				status := "not in the map"
				if m != nil {
					status = m.Status
				}
				wait = append(wait, u+" ("+status+")")
			}
		}
		if len(wait) > 0 {
			return false, "the node uses " + strings.Join(wait, ", ") + ", which is not proved, proved-mod or defined"
		}
		if len(n.Uses) == 0 {
			return true, "the node is " + n.Status + " and uses no node"
		}
		return true, "the node is " + n.Status + " and each node that it uses is proved, proved-mod or defined"
	case "refuter":
		switch n.Status {
		case blueprint.Stated, blueprint.Faithful, blueprint.Open, blueprint.Claimed:
			return true, "the node is " + n.Status + ", so a negation or a counterexample is still possible"
		}
		return false, "the node is " + n.Status + ", and the refuter role acts on a stated, faithful, open or claimed node"
	case "explorer":
		switch {
		case n.Status == blueprint.Open || n.Status == blueprint.Claimed:
			return true, "the node is " + n.Status + ", so the lab can test it"
		case (n.Kind == "example" || n.Kind == "check") && n.Status != blueprint.Proved:
			return true, "an " + n.Kind + " node that is not proved"
		}
		return false, "the explorer role acts on an open or claimed node, or on an example or check that is not proved"
	case "explainer":
		if n.Status == blueprint.Planned || n.Status == blueprint.Refuted {
			return false, "the node is " + n.Status + ", and the explainer role acts on a node with a Lean statement that is not refuted"
		}
		return true, "the node has a Lean statement to explain"
	case "linker":
		if n.Math2 != "" {
			return false, "the node has the Math 2.0 node " + n.Math2
		}
		return true, "the node has no Math 2.0 node"
	}
	return false, "the role " + role + " does not act on a node"
}

// The default weights of the work queue. The route weights are those of spec 04.1. Spec 10.4 says that a library node has the weight of the milestone that needs it, but it gives no number for a milestone. The milestone weights below are a guess of the project [U]: the earlier milestones weigh more, because the later ones need them.
var (
	defaultMilestoneWeights = map[string]float64{"S0": 1, "S1": 1, "S2": 1, "S3": 0.8, "S4": 0.6, "S5": 0.5, "S6": 0.4, "S7": 0.3, "S8": 0.2, "S9": 0.1}
	defaultOtherWeight      = 0.1
)

// defaultRoutes is blueprint/routes.yaml as mmp queue writes it when the file is missing. The text is from the table of spec 04.1.
const defaultRoutes = `# The research routes of mmp, from the table of spec 04.1. mmp queue wrote this file because blueprint/routes.yaml was missing. The weights are the first weights of spec 04.1. They set the share of the research budget of the agent (spec 10.6) and the weight of a route node in the work queue (spec 10.4). People set the weights, and they change at each milestone report (spec 15). The computation lab has the weight 0.05; it is a method, not a route, so it is not in this file.
- id: R1
  title: Formal checks of fresh claims
  weight: 0.20
  idea: Check a self-contained part of each fresh claim of 01.3 in Lean, such as a finite computation, a combinatorial lemma, a numerical inequality or an implication between named hypotheses.
  known: The claims of 01.3 exist as preprints and AI manuscripts. One has a Lean proof.
  obstruction: The claims rest on foundations that are not in Lean, so a check must find a self-contained part.
  first: ""
- id: R2
  title: The audit and the bridge of AxiomMath/MMP
  weight: 0.15
  idea: Read the definitions of AxiomMath/MMP in eight audit steps, then prove in a bridge that the statements of MinimalModel follow from the theorems of AxiomMath/MMP.
  known: A kernel-checked proof of the lc MMP in dimension at most 4, at commit 1f9ade3670. No person has read its definitions against the textbooks.
  obstruction: The library has about 2 million lines. The question is the meaning of a few hundred definitions.
  first: ""
- id: R3
  title: The toric MMP and toric invariants
  weight: 0.15
  idea: Prove the cone theorem, the flips, termination and the MMP for toric varieties on Mathlib alone, as convex geometry of fans.
  known: The toric MMP on paper (Reid 1983, Matsuki 2002, Fujino-Sato 2004) and toric ACC for minimal log discrepancies (Ambro).
  obstruction: Fans are not in Lean, and the dictionary between fans and schemes is large.
  first: ""
- id: R4
  title: Termination through ACC and LSC
  weight: 0.10
  idea: Prove the reductions of Shokurov and of Han-Liu in Lean, then work on their inputs where they are within reach.
  known: Termination in dimension 3, ACC for minimal log discrepancies of smooth and of fixed 3-folds, and the reduction of Shokurov on paper.
  obstruction: ACC for minimal log discrepancies is open for 3-folds.
  first: ""
- id: R5
  title: Abundance and non-vanishing
  weight: 0.10
  idea: State the conditional abundance results as reductions, and prove the known cases that the handbook needs.
  known: Dimension at most 3, the big case, numerical dimension 0, 4-folds with positive Kodaira dimension, and conditional results.
  obstruction: Non-vanishing has no strategy in dimension 4.
  first: ""
- id: R6
  title: Boundedness, Calabi-Yau, the cone conjecture and effective results
  weight: 0.10
  idea: Reduce a target to a finite search, a lattice computation or a numerical inequality, and check it.
  known: BAB, ACC for log canonical thresholds, the Fujita conjecture for dimension at most 5, and the counterexamples of Ajit.
  obstruction: Calabi-Yau 3-folds and very ampleness.
  first: ""
- id: R7
  title: Characteristic p and mixed characteristic
  weight: 0.05
  idea: State the program in characteristic p with the shared hypothesis HasLogResolution, and check the small cases.
  known: The 3-fold MMP for p at least 5, lc 3-fold abundance for p greater than 42, and arithmetic 3-folds.
  obstruction: Resolution in dimension at least 4, and small primes.
  first: ""
- id: R8
  title: Foliations
  weight: 0.05
  idea: State the foliated program and answer the small questions of the survey of Cascini-Spicer, first on surfaces.
  known: Surfaces, 3-folds, and algebraically integrable foliations in all dimensions.
  obstruction: General foliations in dimension at least 4.
  first: ""
- id: R9
  title: Kahler and analytic
  weight: 0.05
  idea: Check the dependencies of the new Kahler claims, and state the Kahler program as a separate family.
  known: Kahler 3-folds, and big gklt Kahler pairs in all dimensions (claimed).
  obstruction: Currents and Kahler classes are not in Lean.
  first: ""
`

// milestoneFile is blueprint/milestones.yaml: the weights of the library nodes in the work queue.
type milestoneFile struct {
	Milestones map[string]float64 `yaml:"milestones"`
	Other      float64            `yaml:"other"`
}

const milestonesHeader = `# The weights of the library nodes in the work queue of mmp (spec 10.4). A node with no route has the weight of the first milestone of spec 15 that needs it: the first milestone with a top theorem of spec 14.2 that uses the node, directly or through other nodes. A node that no milestone needs has the weight "other". The route nodes have the weights of blueprint/routes.yaml. Spec 10.4 gives no number for a milestone, so these defaults are a guess of the project [U]: the earlier milestones weigh more, because the later ones need them. People set the weights.
`

var milestoneRE = regexp.MustCompile(`^S[0-9]$`)

// queueWeights are the weights of the rank of spec 10.4.
type queueWeights struct {
	Routes     map[string]float64
	Milestones map[string]float64
	Other      float64
}

// ensureWeights writes blueprint/routes.yaml and blueprint/milestones.yaml with the default weights when a file is missing. With write false it writes nothing.
func ensureWeights(content string, write bool, stderr io.Writer) error {
	if _, err := os.Stat(filepath.Join(content, "blueprint")); err != nil {
		return fmt.Errorf("%s is not the content repository: %w", content, err)
	}
	if !write {
		return nil
	}
	ms, err := yaml.Marshal(milestoneFile{Milestones: defaultMilestoneWeights, Other: defaultOtherWeight})
	if err != nil {
		return err
	}
	for _, f := range []struct{ name, text string }{
		{"routes.yaml", defaultRoutes},
		{"milestones.yaml", milestonesHeader + string(ms)},
	} {
		dst := filepath.Join(content, "blueprint", f.name)
		if _, err := os.Stat(dst); err == nil || !errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err := os.WriteFile(dst, []byte(f.text), 0o644); err != nil { // #nosec G306 -- a public file of the content repository
			return err
		}
		fmt.Fprintf(stderr, "wrote %s with the default weights; people set the weights\n", dst)
	}
	return nil
}

// loadWeights reads the weights of the routes from the map and the weights of the milestones from blueprint/milestones.yaml, or the defaults when the file is missing.
func loadWeights(p *blueprint.Project, content string) (queueWeights, error) {
	w := queueWeights{Routes: map[string]float64{}, Milestones: map[string]float64{}, Other: defaultOtherWeight}
	routes := p.Routes
	if len(routes) == 0 {
		if err := yaml.Unmarshal([]byte(defaultRoutes), &routes); err != nil {
			return w, err
		}
	}
	for _, r := range routes {
		w.Routes[r.ID] = r.Weight
	}
	for k, v := range defaultMilestoneWeights {
		w.Milestones[k] = v
	}
	path := filepath.Join(content, "blueprint", "milestones.yaml")
	data, err := os.ReadFile(path) // #nosec G304 -- a file of the content repository
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return w, nil
	case err != nil:
		return w, err
	}
	var m milestoneFile
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&m); err != nil {
		return w, fmt.Errorf("%s: %w", path, err)
	}
	for k, v := range m.Milestones {
		if !milestoneRE.MatchString(k) || v < 0 {
			return w, fmt.Errorf("%s: the milestone %q with the weight %v is not a milestone S0 to S9 with a weight of 0 or more", path, k, v)
		}
		w.Milestones[k] = v
	}
	if m.Other < 0 {
		return w, fmt.Errorf("%s: the weight other is negative", path)
	}
	w.Other = m.Other
	return w, nil
}

// topNode returns the node of a top theorem of spec 14.2: the node whose proof is the theorem or whose statement is the statement of the theorem.
func topNode(p *blueprint.Project, t blueprint.TopTheorem) *blueprint.Node {
	for _, n := range p.Nodes {
		if n.Lean.Proof == t.Theorem || (t.Statement != "" && n.Lean.Statement == t.Statement) {
			return n
		}
	}
	return nil
}

// currentMilestone is the first milestone of spec 14.2 with a top theorem that is not proved.
func currentMilestone(p *blueprint.Project) string {
	last := ""
	for _, t := range blueprint.TopTheorems {
		last = t.Milestone
		if n := topNode(p, t); n == nil || n.Status != blueprint.Proved {
			return t.Milestone
		}
	}
	return last
}

// closure returns the node and each node that it uses, directly or through other nodes.
func closure(p *blueprint.Project, n *blueprint.Node) map[string]bool {
	out := map[string]bool{}
	var visit func(m *blueprint.Node)
	visit = func(m *blueprint.Node) {
		if out[m.Tag] {
			return
		}
		out[m.Tag] = true
		for _, u := range m.Uses {
			if k := p.Node(u); k != nil {
				visit(k)
			}
		}
	}
	visit(n)
	return out
}

// queueItem is a row of the work queue with the terms of its rank.
type queueItem struct {
	Target   string // the node tag or the claim key
	Title    string
	Rank     float64
	Weight   float64
	WhyW     string
	Unblocks []string
	Bonus    int
	WhyBonus string
	Attempts int
	Age      float64
	WhyAge   string
	Act      string // why the role can act on the target
	Node     *blueprint.Node
	Claim    *claimEntry
}

func (it queueItem) reason() string {
	parts := []string{it.WhyW}
	if len(it.Unblocks) > 0 {
		parts = append(parts, fmt.Sprintf("it unblocks %s (%s)", plural(len(it.Unblocks), "node"), strings.Join(it.Unblocks, ", ")))
	} else {
		parts = append(parts, "it unblocks no node")
	}
	parts = append(parts, it.WhyBonus, plural(it.Attempts, "failed run"), it.WhyAge, it.Act)
	return strings.Join(parts, "; ")
}

// queueInput is what the rank of spec 10.4 reads.
type queueInput struct {
	project   *blueprint.Project
	claims    []*claimEntry
	runs      []*runRecord
	weights   queueWeights
	milestone string // the current milestone
	date      time.Time
}

// rankNode computes the rank of spec 10.4 for a node.
func (in queueInput) rankNode(role string, n *blueprint.Node, needs map[string]string, path map[string]bool, top string) queueItem {
	p := in.project
	it := queueItem{Target: n.Tag, Title: n.Title, Node: n}
	if len(n.Route) > 0 {
		best := ""
		for _, r := range n.Route {
			if w, ok := in.weights.Routes[r]; ok && (best == "" || w > it.Weight) {
				best, it.Weight = r, w
			}
		}
		if best == "" {
			it.WhyW = "weight 0: the routes " + strings.Join(n.Route, ", ") + " have no weight in blueprint/routes.yaml"
		} else {
			it.WhyW = fmt.Sprintf("weight %.2f of the route %s", it.Weight, best)
		}
	} else if m := needs[n.Tag]; m != "" {
		it.Weight = in.weights.Milestones[m]
		it.WhyW = fmt.Sprintf("weight %.2f of the milestone %s, the first that needs it", it.Weight, m)
	} else {
		it.Weight = in.weights.Other
		it.WhyW = fmt.Sprintf("weight %.2f: no route and no milestone needs it", it.Weight)
	}
	// unblocks: the nodes that wait only on this node.
	for _, user := range n.UsedBy {
		m := p.Node(user)
		if m == nil || m.Status == blueprint.Proved || m.Status == blueprint.Refuted {
			continue
		}
		only := true
		for _, u := range m.Uses {
			if k := p.Node(u); u != n.Tag && (k == nil || !done(k)) {
				only = false
				break
			}
		}
		if only && !contains(it.Unblocks, user) {
			it.Unblocks = append(it.Unblocks, user)
		}
	}
	sort.Strings(it.Unblocks)
	if path[n.Tag] {
		it.Bonus = 1
		it.WhyBonus = "level bonus 1: on the path to " + top + " of the current milestone " + in.milestone
	} else {
		it.WhyBonus = "level bonus 0: not on the path to a top theorem of " + in.milestone
	}
	in.finish(&it, role)
	return it
}

// rankClaim computes the rank of a fresh claim for the reader role. A claim has the weight of route R1 and unblocks no node.
func (in queueInput) rankClaim(role string, e *claimEntry) queueItem {
	it := queueItem{Target: e.Record.Key, Title: deref(e.Record.Title), Claim: e}
	if it.Title == "" {
		it.Title = sourceText(e.Record.Source)
	}
	it.Weight = in.weights.Routes["R1"]
	it.WhyW = fmt.Sprintf("weight %.2f of the route R1", it.Weight)
	it.WhyBonus = "level bonus 0: a claim is not on the path to a top theorem"
	it.Act = "the claim is " + e.Record.Status + " and has " + plural(len(e.Record.Parts), "part")
	in.finish(&it, role)
	return it
}

// finish adds the failed runs and the age factor and computes the rank. The age factor is not defined in spec 10.4; mmp uses 1 plus the days since the last run on the target, divided by 28, at most 2. A target with no run has the factor 2, so a target that waits long rises in the queue.
func (in queueInput) finish(it *queueItem, role string) {
	last := ""
	for _, r := range in.runs {
		if r.Target != it.Target {
			continue
		}
		if r.Role == role && r.Status == runFailed {
			it.Attempts++
		}
		if r.Date > last {
			last = r.Date
		}
	}
	it.Age = 2
	it.WhyAge = "age factor 2.00: no run yet"
	if last != "" {
		if t, err := time.Parse("2006-01-02", last); err == nil {
			days := in.date.Sub(t).Hours() / 24
			days = max(0, min(days, 28))
			it.Age = 1 + days/28
			it.WhyAge = fmt.Sprintf("age factor %.2f: the last run was on %s", it.Age, last)
		}
	}
	it.Rank = it.Weight * float64(1+len(it.Unblocks)) * float64(1+it.Bonus) / float64(1+it.Attempts) * it.Age
}

// queue returns the work queue of the role, highest rank first. Two items with the same rank are in the order of their tag or key, so the queue is the same on each run.
func (in queueInput) queue(role *agentRole) []queueItem {
	var out []queueItem
	switch role.Target {
	case "claim":
		for _, e := range in.claims {
			if e.Record.Status == "fresh" || e.Record.Status == "reading" {
				out = append(out, in.rankClaim(role.Name, e))
			}
		}
	case "node":
		needs, path, top := in.paths()
		for _, n := range in.project.Nodes {
			if ok, why := canAct(in.project, role.Name, n); ok {
				it := in.rankNode(role.Name, n, needs, path, top)
				it.Act = "the role can act: " + why
				out = append(out, it)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Rank != out[j].Rank {
			return out[i].Rank > out[j].Rank
		}
		return out[i].Target < out[j].Target
	})
	return out
}

// paths returns the first milestone that needs each node, the nodes on the path to a top theorem of the current milestone, and the names of those theorems.
func (in queueInput) paths() (map[string]string, map[string]bool, string) {
	needs := map[string]string{}
	path := map[string]bool{}
	var tops []string
	for _, t := range blueprint.TopTheorems {
		n := topNode(in.project, t)
		if n == nil {
			continue
		}
		for tag := range closure(in.project, n) {
			if old, ok := needs[tag]; !ok || t.Milestone < old {
				needs[tag] = t.Milestone
			}
			if t.Milestone == in.milestone {
				path[tag] = true
			}
		}
		if t.Milestone == in.milestone {
			tops = append(tops, t.Theorem)
		}
	}
	top := strings.Join(tops, ", ")
	if top == "" {
		top = "no top theorem in the map"
	}
	return needs, path, top
}

// loadQueue reads what the queue needs from the content repository.
func loadQueue(content, milestone, date string, write bool, stderr io.Writer) (queueInput, error) {
	var in queueInput
	if err := ensureWeights(content, write, stderr); err != nil {
		return in, err
	}
	p, err := blueprint.Load(content)
	if err != nil {
		return in, err
	}
	in.project = p
	if in.weights, err = loadWeights(p, content); err != nil {
		return in, err
	}
	if in.claims, err = loadClaims(content); err != nil {
		return in, err
	}
	if in.runs, err = loadRuns(content); err != nil {
		return in, err
	}
	if date == "" {
		date = time.Now().UTC().Format("2006-01-02")
	}
	if in.date, err = time.Parse("2006-01-02", date); err != nil || !dateRE.MatchString(date) {
		return in, fmt.Errorf("%w: the date %q is not YYYY-MM-DD", errUsage, date)
	}
	if milestone == "" {
		milestone = currentMilestone(p)
	} else if !milestoneRE.MatchString(milestone) {
		return in, fmt.Errorf("%w: the milestone %q is not S0 to S9", errUsage, milestone)
	}
	in.milestone = milestone
	return in, nil
}

func runQueue(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("mmp queue", flag.ContinueOnError)
	fs.SetOutput(stderr)
	content := fs.String("content", ".", "the root of the content repository")
	roleName := fs.String("role", "prover", "the role: "+roleNames())
	n := fs.Int("n", 20, "print at most this many items; 0 prints all")
	milestone := fs.String("milestone", "", "the current milestone, for the level bonus (default: the first milestone with a top theorem that is not proved)")
	date := fs.String("date", "", "the date for the age factor, YYYY-MM-DD (default: today, UTC)")
	if err := parse(fs, args); err != nil {
		return err
	}
	if *n < 0 {
		return fmt.Errorf("%w: -n is negative", errUsage)
	}
	role, err := queueRole(*roleName)
	if err != nil {
		return err
	}
	in, err := loadQueue(*content, *milestone, *date, true, stderr)
	if err != nil {
		return err
	}
	items := in.queue(role)
	what := "node"
	if role.Target == "claim" {
		what = "claim"
	}
	fmt.Fprintf(stdout, "the queue of the role %s: %s; the current milestone is %s\n", role.Name, plural(len(items), what), in.milestone)
	fmt.Fprintln(stdout, "rank = weight x (1 + unblocks) x (1 + level bonus) / (1 + failed runs) x age factor (spec 10.4)")
	for i, it := range items {
		if *n > 0 && i == *n {
			fmt.Fprintf(stdout, "and %d more; use -n 0 to print all\n", len(items)-*n)
			break
		}
		fmt.Fprintf(stdout, "%3d. %7.3f  %s  %s\n     %s\n", i+1, it.Rank, it.Target, it.Title, it.reason())
	}
	return nil
}
