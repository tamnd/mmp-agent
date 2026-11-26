// Package lab runs the experiments of the computation lab, spec 13. A spec is lab/specs/<name>.yaml of the content repository, and a run is lab/runs/<id>/ with record.json, summary.md and the outputs.
package lab

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// Experiment is a row of the table of spec 13.3.
type Experiment struct {
	ID       string
	Title    string
	Route    string
	Runnable bool // mmp lab run has code for it
}

// Experiments are the experiments of spec 13.3. Only E2 has code yet.
var Experiments = []Experiment{
	{"E1", "baskets and plurigenera of 3-folds of general type, and the volume bound 1/1680", "R1", false},
	{"E2", "toric mlds and enc germs of the cyclic quotients of dimension 3 and 4", "R3, R4", true},
	{"E3", "the arithmetic of the counterexamples of Ajit to effective non-vanishing", "R1", false},
	{"E4", "the questions of Ajit on weighted hypersurfaces with no sections", "R6", false},
	{"E5", "the volumes of Fano 3-folds and of 3-folds of general type", "R6", false},
	{"E6", "the cone conjecture for one deformation class of Calabi-Yau 3-folds", "R6", false},
	{"E7", "F-purity and the mlds of quotient germs in characteristic p", "R7", false},
	{"E8", "the explicit termination bounds of Han-Liu-Zhuang in dimension at most 4", "R4", false},
	{"E9", "random runs of the toric MMP in dimension 3 and 4", "R3, library", false},
	{"E10", "the explicit identities and numbers of each new claim of route R1", "R1", false},
}

// FindExperiment returns the experiment with the id, or nil.
func FindExperiment(id string) *Experiment {
	for i := range Experiments {
		if Experiments[i].ID == id {
			return &Experiments[i]
		}
	}
	return nil
}

// Tool is the value of tool in a spec that mmp runs itself.
const Tool = "mmp"

// Spec is a lab job, spec 13.2.
type Spec struct {
	Name       string   `yaml:"name"`
	Experiment string   `yaml:"experiment"`
	Question   string   `yaml:"question"`
	Tool       string   `yaml:"tool"`
	Node       string   `yaml:"node"`
	Nodes      []string `yaml:"nodes,omitempty"`
	Inputs     E2Inputs `yaml:"inputs"`
	Outputs    []string `yaml:"outputs"`
	Notes      string   `yaml:"notes,omitempty"`

	Path string `yaml:"-"` // the file of the spec
	Hash string `yaml:"-"` // the sha256 of the bytes of the file
}

var nameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// ParseSpec reads a spec from its bytes. The inputs have the form of the experiment, so a spec of an experiment with no code is an error.
func ParseSpec(path string, data []byte) (*Spec, error) {
	var s Spec
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&s); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	sum := sha256.Sum256(data)
	s.Path, s.Hash = path, hex.EncodeToString(sum[:])
	e := FindExperiment(s.Experiment)
	switch {
	case !nameRE.MatchString(s.Name):
		return nil, fmt.Errorf("%s: the name %q must have only lower case letters, digits and hyphens", path, s.Name)
	case e == nil:
		return nil, fmt.Errorf("%s: the experiment %q is not one of E1 to E10 of spec 13.3", path, s.Experiment)
	case !e.Runnable:
		return nil, fmt.Errorf("%s: mmp has no code for experiment %s yet", path, s.Experiment)
	case s.Tool != Tool:
		return nil, fmt.Errorf("%s: the tool is %q, but mmp lab runs only the specs with the tool %s", path, s.Tool, Tool)
	case strings.TrimSpace(s.Question) == "" || s.Node == "":
		return nil, fmt.Errorf("%s: the spec needs a question and a node", path)
	}
	if err := s.Inputs.check(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &s, nil
}

// LoadSpec reads a spec file.
func LoadSpec(path string) (*Spec, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- a spec that the user names
	if err != nil {
		return nil, err
	}
	return ParseSpec(path, data)
}

// SpecE2 is the spec of E2 that mmp lab spec E2 prints. A person copies it to lab/specs/ of the content repository.
const SpecE2 = `name: e2-cyclic-quotient-mld-3
experiment: E2
question: For each isolated cyclic quotient 3-fold germ 1/r(a, b, c) with r <= 60, is the germ terminal exactly when two weights add to 0 mod r (the terminal lemma), and is the mld of each terminal germ 1 + 1/r?
tool: mmp
node: MMXCD16PTN # HanLiuEncACC, Target T
nodes: [MMWK2DDDPC, MMAWP4SA44] # ACCMld, ToricMMP
inputs:
  dimension: 3
  max_index: 60
outputs: [germs.json, indices.json, summary.md]
notes: A small version of E2 in Go, with no container and no network. It covers the isolated cyclic quotient germs, which are the simplicial toric germs with one box generator. It has no Lean certificate yet (13.4).
`

// Record is record.json of a run. The site reads the fields of site.LabRecord, and the other fields make the run reproducible.
type Record struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Experiment  string            `json:"experiment"`
	Question    string            `json:"question"`
	Spec        string            `json:"spec"`
	SpecSHA256  string            `json:"spec_sha256"`
	Node        string            `json:"node"`
	Nodes       []string          `json:"nodes,omitempty"`
	Tool        string            `json:"tool"`
	ToolVersion string            `json:"tool_version"`
	Commit      string            `json:"commit,omitempty"`
	Modified    bool              `json:"modified,omitempty"` // the binary was built from a checkout with changes
	Go          string            `json:"go"`
	Container   string            `json:"container"` // the image digest of spec 13.2, or none
	Network     bool              `json:"network"`
	Seed        uint64            `json:"seed"`
	Host        string            `json:"host"`
	Started     string            `json:"started"`
	WallSeconds float64           `json:"wall_seconds"`
	Result      string            `json:"result"`
	Mismatches  int               `json:"mismatches"` // the outputs that contradict the prediction
	Gaps        int               `json:"gaps"`       // the predicted classes with no example in the run
	Files       map[string]string `json:"files"`      // the sha256 of each file of the run, record.json excluded
}

// Options are the settings of a run that are not in the spec.
type Options struct {
	Runs        string // the directory of the runs
	Force       bool   // replace a run with the same id
	Host        string
	ToolVersion string
	Commit      string
	Modified    bool
	Go          string
	Now         func() time.Time
}

// RunsDir returns lab/runs next to the lab/specs of a spec path, or "" when the spec is not in lab/specs.
func RunsDir(spec string) string {
	abs, err := filepath.Abs(spec)
	if err != nil {
		return ""
	}
	dir := filepath.Dir(abs)
	if filepath.Base(dir) == "specs" && filepath.Base(filepath.Dir(dir)) == "lab" {
		return filepath.Join(filepath.Dir(dir), "runs")
	}
	return ""
}

// specRel returns the path of a spec from the root of the content repository, when the path has lab/specs/ in it.
func specRel(path string) string {
	p := filepath.ToSlash(path)
	if i := strings.LastIndex(p, "lab/specs/"); i >= 0 {
		return p[i:]
	}
	return filepath.Base(p)
}

// Run runs a spec and writes the run directory. It returns the record. A run whose outputs contradict the prediction is written too; the caller reads Mismatches.
func Run(s *Spec, opt Options) (*Record, error) {
	if opt.Now == nil {
		opt.Now = time.Now
	}
	if opt.Runs == "" {
		return nil, errors.New("lab: the directory of the runs is not set")
	}
	start := opt.Now().UTC()
	rec := &Record{
		Name: s.Name, Experiment: s.Experiment, Question: s.Question, Spec: specRel(s.Path), SpecSHA256: s.Hash,
		Node: s.Node, Nodes: s.Nodes, Tool: Tool, ToolVersion: opt.ToolVersion, Commit: opt.Commit, Modified: opt.Modified,
		Go: opt.Go, Container: "none", Host: opt.Host, Started: start.Format(time.RFC3339), Files: map[string]string{},
	}
	rec.ID = start.Format("2006-01-02") + "-" + s.Name + "-" + s.Hash[:8]
	dir := filepath.Join(opt.Runs, rec.ID)
	if _, err := os.Stat(dir); err == nil && !opt.Force {
		return nil, fmt.Errorf("lab: the run %s exists; use -force to replace it", dir)
	}
	res, err := RunE2(s.Inputs)
	if err != nil {
		return nil, err
	}
	rec.Mismatches, rec.Gaps = res.Mismatches(), res.Gaps()
	rec.Result = e2Result(res)
	files := map[string][]byte{
		"germs.json":   marshal(res.Germs),
		"indices.json": marshal(res.Indices),
	}
	rec.WallSeconds = float64(opt.Now().Sub(start).Milliseconds()) / 1000
	files["summary.md"] = []byte(e2Summary(s, rec, res))
	if err := os.RemoveAll(dir); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, err
	}
	for n, data := range files {
		if err := os.WriteFile(filepath.Join(dir, n), data, 0o644); err != nil { // #nosec G306 -- public data
			return nil, err
		}
		sum := sha256.Sum256(data)
		rec.Files[n] = hex.EncodeToString(sum[:])
	}
	if err := os.WriteFile(filepath.Join(dir, "record.json"), marshal(rec), 0o644); err != nil { // #nosec G306 -- public data
		return nil, err
	}
	return rec, nil
}

func marshal(v any) []byte {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		panic(err) // the types of this package always marshal
	}
	return append(data, '\n')
}

// e2Result is the one-line result of a run of E2.
func e2Result(r E2Result) string {
	switch {
	case r.Mismatches() > 0:
		return fmt.Sprintf("mismatch: %d germs contradict the terminal lemma and %d terminal germs do not have the mld 1 + 1/r", len(r.LemmaFails), len(r.MldFails))
	case r.Gaps() > 0:
		return fmt.Sprintf("incomplete: %d indices have no terminal germ", r.Gaps())
	case r.Dimension == 3:
		return fmt.Sprintf("agrees: in %d germs of dimension 3, a germ is terminal exactly when two weights add to 0 mod r, and each terminal germ has the mld 1 + 1/r", len(r.Germs))
	}
	return fmt.Sprintf("recorded: %d germs of dimension %d, with %d distinct mlds below 1; dimension %d has no prediction", len(r.Germs), r.Dimension, len(r.Values), r.Dimension)
}

func e2Summary(s *Spec, rec *Record, r E2Result) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", rec.ID)
	fmt.Fprintf(&b, "%s\n\n", s.Question)
	fmt.Fprintf(&b, "Result: %s.\n\n", rec.Result)
	b.WriteString("## What the run does\n\n")
	fmt.Fprintf(&b, "The run lists each isolated cyclic quotient germ 1/r(a_1, ..., a_%d) with 2 <= r <= %d, once up to the order of the weights and the choice of the generator of the group. The germ is the toric variety of the cone spanned by e_1, ..., e_n in the lattice Z^n + Z (a_1, ..., a_n)/r. Because the germ is isolated, each exceptional divisor of log discrepancy at most 1 comes from a point of the box, and the log discrepancy of the point of k is the age, the sum of the fractional parts of k a_i / r. So the mld at the origin is the smallest age, and the germ is terminal when each age is more than 1 and canonical when each age is at least 1 (the criterion of Reid and Tai).\n\n", r.Dimension, s.Inputs.MaxIndex)
	if r.Dimension == 3 {
		b.WriteString("The run checks two known results. The terminal lemma (Morrison-Stevens, White-Frumkin) says that the germ is terminal if and only if two of the weights add to 0 mod r. For a terminal germ 1/r(1, -1, a), the age of the k with k a = 1 mod r is 1 + 1/r, so the mld is 1 + 1/r.\n\n")
	} else {
		b.WriteString("Dimension 4 has no prediction in this run. The counts are the data for Target T (spec 04.12): a germ with exactly one exceptional divisor of log discrepancy at most 1 is counted as enc. The definition of enc in Han-Liu must be checked against this count before a node uses it [U].\n\n")
	}
	b.WriteString("## The counts by index\n\n| r | germs | terminal | canonical | enc |\n|---|---|---|---|---|\n")
	for _, x := range r.Indices {
		fmt.Fprintf(&b, "| %d | %d | %d | %d | %d |\n", x.Index, x.Germs, x.Terminal, x.Canonical, x.Enc)
	}
	fmt.Fprintf(&b, "\n## The mlds below 1\n\nThe run found %d distinct values: %s.\n", len(r.Values), list(r.Values))
	if r.Mismatches() > 0 {
		b.WriteString("\n## The germs that contradict a prediction\n\n")
		for _, g := range append(append([]Germ(nil), r.LemmaFails...), r.MldFails...) {
			fmt.Fprintf(&b, "- 1/%d%v: mld %d/%d, terminal %s\n", g.Index, g.Weights, g.MldNum, g.MldDen, yes(g.Terminal))
		}
	}
	b.WriteString("\n## What the run does not do\n\n")
	b.WriteString("- It is a search, not a proof. Only a certificate that Lean checks makes a theorem (spec 13.4).\n")
	b.WriteString("- It covers only the isolated cyclic quotients, not every simplicial toric germ and not the germs that are not simplicial.\n")
	b.WriteString("- It runs in the mmp binary, not in a container with pinned images, and it uses no network and no other tool. The record gives the commit of mmp, so the outputs are reproducible.\n")
	return b.String()
}

func list(xs []string) string {
	if len(xs) == 0 {
		return "none"
	}
	return strings.Join(xs, ", ")
}

func yes(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// LoadRun reads record.json of a run directory. The id must be the name of the directory.
func LoadRun(dir string) (*Record, error) {
	data, err := os.ReadFile(filepath.Join(dir, "record.json")) // #nosec G304 -- a file of the content repository
	if err != nil {
		return nil, err
	}
	var rec Record
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("%s: %w", dir, err)
	}
	if rec.ID == "" || rec.ID != filepath.Base(dir) {
		return nil, fmt.Errorf("%s: the id %q of record.json is not the name of the directory", dir, rec.ID)
	}
	return &rec, nil
}

// LoadRuns reads each run of the directory, sorted by id. A missing directory has no runs.
func LoadRuns(dir string) ([]*Record, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []*Record
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		rec, err := LoadRun(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// SpecInfo is a spec file of lab/specs, for mmp lab list. Err is set when the file does not parse.
type SpecInfo struct {
	Path string
	Spec *Spec
	Err  error
}

// LoadSpecs reads each YAML file of the directory. A missing directory has no specs.
func LoadSpecs(dir string) ([]SpecInfo, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []SpecInfo
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		s, err := LoadSpec(path)
		out = append(out, SpecInfo{Path: path, Spec: s, Err: err})
	}
	return out, nil
}
