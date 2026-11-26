package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/tamnd/mmp-agent/blueprint"
)

// auditDecls are the seven declarations of step A2 of spec 04.3: the four main theorems and the three other results of AxiomMath/MMP.
var auditDecls = []string{
	"MMP.Fourfold.Program.IsMaximal.isNefOnCurves_or_isMoriFibreSpace",
	"MMP.Fourfold.Program.isContinuableRay",
	"MMP.Fourfold.Program.n_ne_top_of_isAlgClosed",
	"MMP.Fourfold.projectiveLowerLogMMP_three",
	"MMP.Mori.subsingleton_derivedCohomologyOverField_divisorialSheaf_neg",
	"MMP.Fourfold.rationalBirationalProgramProperty_two",
	"MMP.Mori.IsKltPair.exists_relativeClosedConeOfCurves_eq_add_hull",
}

func runAudit(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("%w: audit needs a subcommand: status, step, finding or axioms", errUsage)
	}
	switch args[0] {
	case "status":
		return auditStatus(args[1:], stdout, stderr)
	case "step":
		return auditStep(args[1:], stdout, stderr)
	case "finding":
		return auditFinding(args[1:], stdout, stderr)
	case "axioms":
		return auditAxioms(args[1:], stdout, stderr)
	}
	return fmt.Errorf("%w: unknown audit subcommand %q; use status, step, finding or axioms", errUsage, args[0])
}

// loadAudit reads records/audit.json of the content repository.
func loadAudit(content string) (*blueprint.Audit, error) {
	data, err := os.ReadFile(filepath.Join(content, "records", "audit.json")) // #nosec G304 -- a file of the content repository
	if err != nil {
		return nil, fmt.Errorf("the audit has no record: %w", err)
	}
	var a blueprint.Audit
	if err := json.Unmarshal(data, &a); err != nil {
		return nil, fmt.Errorf("records/audit.json: %w", err)
	}
	if a.Steps == nil {
		a.Steps = map[string]string{}
	}
	if a.Findings == nil {
		a.Findings = map[string]int{}
	}
	return &a, nil
}

// saveAudit writes records/audit.json. It sets Passed to false when a step is open or a finding of severity 1 or 2 is open, because a person sets Passed only on a complete audit.
func saveAudit(content string, a *blueprint.Audit) error {
	for _, s := range blueprint.AuditSteps {
		if a.Steps[s] != "done" {
			a.Passed = false
		}
	}
	if a.SeriousFindings() > 0 {
		a.Passed = false
	}
	out, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(content, "records", "audit.json"), append(out, '\n'), 0o644) // #nosec G306 -- public data
}

func auditStatus(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("mmp audit status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	content := fs.String("content", ".", "the root of the content repository")
	if err := fs.Parse(args); err != nil {
		return usageErr(err)
	}
	a, err := loadAudit(*content)
	if err != nil {
		return err
	}
	recs, err := blueprint.AuditRecords(*content)
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "source: %s at %s\n", a.Source, a.Commit)
	if a.Date != "" {
		fmt.Fprintf(stdout, "date:   %s\n", a.Date)
	}
	for _, s := range blueprint.AuditSteps {
		state := a.Steps[s]
		if state == "" {
			state = "not started"
		}
		var files []string
		for _, r := range recs {
			if r.Step == s {
				files = append(files, r.Path)
			}
		}
		line := fmt.Sprintf("%s  %-11s", s, state)
		if len(files) > 0 {
			line += "  " + strings.Join(files, ", ")
		}
		fmt.Fprintln(stdout, strings.TrimRight(line, " "))
	}
	var parts []string
	for _, sev := range blueprint.AuditSeverities {
		parts = append(parts, fmt.Sprintf("%d of severity %s", a.Findings[sev], sev))
	}
	fmt.Fprintf(stdout, "open findings: %s\n", strings.Join(parts, ", "))
	fmt.Fprintf(stdout, "passed: %s\n", yesNo(a.Passed))
	return nil
}

func auditStep(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("mmp audit step", flag.ContinueOnError)
	fs.SetOutput(stderr)
	content := fs.String("content", ".", "the root of the content repository")
	date := fs.String("date", "", "the date of the record, as YYYY-MM-DD (default: today, in UTC)")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: mmp audit step [flags] <A1 to A8> <done | \"in progress\" | \"not started\">")
		fmt.Fprintln(stderr, "A step is done only when its exit record is in audit/. Two people sign the records of A5 and A6.")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return usageErr(err)
	}
	if fs.NArg() != 2 {
		return fmt.Errorf("%w: audit step needs a step and a state, after the flags", errUsage)
	}
	step, state := strings.ToUpper(fs.Arg(0)), fs.Arg(1)
	if !slices.Contains(blueprint.AuditSteps, step) {
		return fmt.Errorf("%w: the step %q is not one of %s", errUsage, fs.Arg(0), strings.Join(blueprint.AuditSteps, ", "))
	}
	if !slices.Contains(blueprint.AuditStates, state) {
		return fmt.Errorf("%w: the state %q is not one of %s", errUsage, state, strings.Join(blueprint.AuditStates, ", "))
	}
	d, err := recordDate(*date)
	if err != nil {
		return err
	}
	a, err := loadAudit(*content)
	if err != nil {
		return err
	}
	if state == "done" {
		recs, err := blueprint.AuditRecords(*content)
		if err != nil {
			return err
		}
		if !slices.ContainsFunc(recs, func(r blueprint.AuditRecord) bool { return r.Step == step }) {
			return fmt.Errorf("the step %s has no exit record in audit/, so it cannot be done; add a file whose name starts with %s-", step, step)
		}
	}
	a.Steps[step], a.Date = state, d
	if err := saveAudit(*content, a); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "%s is %s in records/audit.json\n", step, state)
	return nil
}

func auditFinding(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("mmp audit finding", flag.ContinueOnError)
	fs.SetOutput(stderr)
	content := fs.String("content", ".", "the root of the content repository")
	severity := fs.Int("severity", 0, "the severity of the finding, 1 to 4 (spec 14.5)")
	date := fs.String("date", "", "the date of the record, as YYYY-MM-DD (default: today, in UTC)")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: mmp audit finding -severity N <add | close>")
		fmt.Fprintln(stderr, "add counts one more open finding of the severity, and close counts one less. The text of the finding is a file of audit/.")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return usageErr(err)
	}
	if fs.NArg() != 1 || (fs.Arg(0) != "add" && fs.Arg(0) != "close") {
		return fmt.Errorf("%w: audit finding needs add or close, after the flags", errUsage)
	}
	sev := strconv.Itoa(*severity)
	if !slices.Contains(blueprint.AuditSeverities, sev) {
		return fmt.Errorf("%w: the severity must be 1, 2, 3 or 4", errUsage)
	}
	d, err := recordDate(*date)
	if err != nil {
		return err
	}
	a, err := loadAudit(*content)
	if err != nil {
		return err
	}
	if fs.Arg(0) == "add" {
		a.Findings[sev]++
	} else {
		if a.Findings[sev] == 0 {
			return fmt.Errorf("the audit has no open finding of severity %s", sev)
		}
		a.Findings[sev]--
	}
	a.Date = d
	if err := saveAudit(*content, a); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "the audit has %s of severity %s\n", plural(a.Findings[sev], "open finding"), sev)
	return nil
}

// recordDate returns the date of a record: the flag value, checked, or today in UTC.
func recordDate(s string) (string, error) {
	if s == "" {
		return time.Now().UTC().Format("2006-01-02"), nil
	}
	if _, err := time.Parse("2006-01-02", s); err != nil {
		return "", fmt.Errorf("%w: the date %q is not YYYY-MM-DD", errUsage, s)
	}
	return s, nil
}

// AxiomRow is one declaration of the axiom report of step A2.
type AxiomRow struct {
	Name     string   `json:"name"`
	Found    bool     `json:"found"`
	Axioms   []string `json:"axioms"`
	Standard bool     `json:"standard"` // the axioms are a subset of propext, Classical.choice and Quot.sound
}

// AxiomReport is the axiom report of step A2, A2.json.
type AxiomReport struct {
	Source    string     `json:"source"`
	Commit    string     `json:"commit"`
	Toolchain string     `json:"toolchain"`
	Date      string     `json:"date"`
	Tool      string     `json:"tool"`
	Rows      []AxiomRow `json:"rows"`
}

// runLean runs a Lean file in the environment of a Lake package. A test replaces it.
var runLean = func(dir, file string) ([]byte, error) {
	cmd := exec.Command("lake", "env", "lean", file) // #nosec G204 -- the file is written by mmp
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

var (
	axiomsRe   = regexp.MustCompile(`(?s)'([^']+)' depends on axioms: \[([^\]]*)\]`)
	noAxiomsRe = regexp.MustCompile(`'([^']+)' does not depend on any axioms`)
)

// parseAxioms reads the output of #print axioms. The key is the name that Lean prints.
func parseAxioms(out string) map[string][]string {
	got := map[string][]string{}
	for _, m := range axiomsRe.FindAllStringSubmatch(out, -1) {
		var ax []string
		for _, a := range strings.Split(m[2], ",") {
			if a = strings.TrimSpace(a); a != "" {
				ax = append(ax, a)
			}
		}
		got[m[1]] = ax
	}
	for _, m := range noAxiomsRe.FindAllStringSubmatch(out, -1) {
		got[m[1]] = []string{}
	}
	return got
}

func auditAxioms(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("mmp audit axioms", flag.ContinueOnError)
	fs.SetOutput(stderr)
	source := fs.String("source", "", "the root of the built Lake package of the external library")
	imp := fs.String("import", "MMP", "the module to import")
	name := fs.String("name", "AxiomMath/MMP", "the name of the external library in the report")
	logOut := fs.String("log", "", "write the full output of Lean to this file")
	jsonOut := fs.String("json", "", "write the report as JSON to this file")
	var decls multiFlag
	fs.Var(&decls, "decl", "a declaration to check; give -decl again for more than one (default: the seven declarations of step A2)")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: mmp audit axioms -source DIR [flags]")
		fmt.Fprintln(stderr, "Step A2 of spec 04.3: run #print axioms on the declarations in the built environment, and compare each list with propext, Classical.choice and Quot.sound.")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return usageErr(err)
	}
	if *source == "" {
		return fmt.Errorf("%w: audit axioms needs -source", errUsage)
	}
	if len(decls) == 0 {
		decls = auditDecls
	}
	var src strings.Builder
	fmt.Fprintf(&src, "import %s\n\n", *imp)
	for _, d := range decls {
		fmt.Fprintf(&src, "#print axioms %s\n", d)
	}
	dir := filepath.Join(*source, ".lake")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "mmp-audit-*.lean")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if _, err := f.WriteString(src.String()); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	out, runErr := runLean(*source, f.Name())
	if *logOut != "" {
		if err := os.WriteFile(*logOut, out, 0o644); err != nil { // #nosec G306 -- public data
			return err
		}
	}
	got := parseAxioms(string(out))
	rep := AxiomReport{Source: *name, Commit: commitOrEmpty(*source), Toolchain: readTrim(filepath.Join(*source, "lean-toolchain")), Date: time.Now().UTC().Format("2006-01-02"), Tool: "mmp " + Version}
	bad := 0
	for _, d := range decls {
		ax, ok := got[d]
		row := AxiomRow{Name: d, Found: ok, Axioms: ax, Standard: ok}
		if row.Axioms == nil {
			row.Axioms = []string{}
		}
		for _, a := range ax {
			if !blueprint.StandardAxioms[a] {
				row.Standard = false
			}
		}
		if !row.Standard {
			bad++
		}
		rep.Rows = append(rep.Rows, row)
	}
	for _, r := range rep.Rows {
		state := "standard"
		switch {
		case !r.Found:
			state = "not found"
		case !r.Standard:
			state = "NOT STANDARD"
		}
		fmt.Fprintf(stdout, "%-12s %s: [%s]\n", state, r.Name, strings.Join(r.Axioms, ", "))
	}
	if *jsonOut != "" {
		data, err := json.MarshalIndent(rep, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(*jsonOut, append(data, '\n'), 0o644); err != nil { // #nosec G306 -- public data
			return err
		}
	}
	if runErr != nil && len(got) == 0 {
		return fmt.Errorf("lake env lean failed: %w\n%s", runErr, out)
	}
	if bad > 0 {
		return fmt.Errorf("%s of %d are not found or use an axiom that is not standard", plural(bad, "declaration"), len(decls))
	}
	fmt.Fprintf(stdout, "all %d declarations use the standard axioms only\n", len(decls))
	return nil
}

// readTrim returns the text of a file without the spaces at the ends, or "".
func readTrim(path string) string {
	data, err := os.ReadFile(path) // #nosec G304 -- a file of the source checkout
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// commitOrEmpty returns the commit of the checkout in dir, or "" when dir is not a git checkout.
func commitOrEmpty(dir string) string {
	c, err := gitHead(dir)
	if err != nil {
		return ""
	}
	return c
}
