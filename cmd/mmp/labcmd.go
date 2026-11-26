package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"

	"github.com/tamnd/mmp-agent/blueprint"
	"github.com/tamnd/mmp-agent/lab"
)

func runLab(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("%w: lab needs a subcommand: list, run, spec or show", errUsage)
	}
	switch args[0] {
	case "list":
		return labList(args[1:], stdout, stderr)
	case "run":
		return labRun(args[1:], stdout, stderr)
	case "spec":
		return labSpec(args[1:], stdout, stderr)
	case "show":
		return labShow(args[1:], stdout, stderr)
	}
	return fmt.Errorf("%w: unknown lab subcommand %q; use list, run, spec or show", errUsage, args[0])
}

func labList(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("mmp lab list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	content := fs.String("content", ".", "the root of the content repository")
	if err := parse(fs, args); err != nil {
		return err
	}
	fmt.Fprintln(stdout, "experiments of spec 13.3:")
	for _, e := range lab.Experiments {
		state := "no code yet"
		if e.Runnable {
			state = "mmp runs a small version"
		}
		fmt.Fprintf(stdout, "  %s  %-25s %s (%s)\n", e.ID, state, e.Title, e.Route)
	}
	specs, err := lab.LoadSpecs(filepath.Join(*content, "lab", "specs"))
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "specs in lab/specs: %d\n", len(specs))
	for _, s := range specs {
		if s.Err != nil {
			fmt.Fprintf(stdout, "  %s  cannot run: %v\n", filepath.Base(s.Path), s.Err)
			continue
		}
		fmt.Fprintf(stdout, "  %s  %s  %s\n", filepath.Base(s.Path), s.Spec.Experiment, s.Spec.Node)
	}
	runs, err := lab.LoadRuns(filepath.Join(*content, "lab", "runs"))
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "runs in lab/runs: %d\n", len(runs))
	for _, r := range runs {
		fmt.Fprintf(stdout, "  %s  %s  %s\n", r.ID, r.Experiment, r.Result)
	}
	return nil
}

// toolInfo returns the commit of this binary and whether the checkout had changes.
func toolInfo() (commit string, modified bool) {
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				commit = s.Value
			case "vcs.modified":
				modified = s.Value == "true"
			}
		}
	}
	return commit, modified
}

// resolveSpec returns the spec file of an argument: a path, or an experiment id such as E2, which names the one spec of that experiment in lab/specs.
func resolveSpec(content, arg string) (string, error) {
	if lab.FindExperiment(arg) == nil {
		return arg, nil
	}
	specs, err := lab.LoadSpecs(filepath.Join(content, "lab", "specs"))
	if err != nil {
		return "", err
	}
	var paths []string
	for _, s := range specs {
		if s.Err == nil && s.Spec.Experiment == arg {
			paths = append(paths, s.Path)
		}
	}
	switch len(paths) {
	case 1:
		return paths[0], nil
	case 0:
		if e := lab.FindExperiment(arg); !e.Runnable {
			return "", fmt.Errorf("mmp has no code for experiment %s yet", arg)
		}
		return "", fmt.Errorf("lab/specs has no spec of %s; write one with: mmp lab spec %s > %s", arg, arg, filepath.Join(content, "lab", "specs", "e2-cyclic-quotient-mld-3.yaml"))
	}
	return "", fmt.Errorf("lab/specs has %d specs of %s (%s); name the file", len(paths), arg, strings.Join(paths, ", "))
}

func labRun(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("mmp lab run", flag.ContinueOnError)
	fs.SetOutput(stderr)
	content := fs.String("content", ".", "the root of the content repository, for an experiment id and for the check of the node")
	runs := fs.String("runs", "", "the directory of the runs (default: lab/runs next to the lab/specs of the spec)")
	force := fs.Bool("force", false, "replace a run with the same id")
	host := fs.String("host", "", "the name of the host in the record (default: the host name)")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: mmp lab run [flags] <spec.yaml | E2>")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return usageErr(err)
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("%w: lab run needs one spec file or one experiment id, after the flags", errUsage)
	}
	path, err := resolveSpec(*content, fs.Arg(0))
	if err != nil {
		return err
	}
	spec, err := lab.LoadSpec(path)
	if err != nil {
		return err
	}
	if p, err := blueprint.Load(*content); err == nil {
		for _, tag := range append([]string{spec.Node}, spec.Nodes...) {
			if p.Node(tag) == nil {
				return fmt.Errorf("%s: the node %s is not in the map of %s", path, tag, *content)
			}
		}
	}
	if *runs == "" {
		if *runs = lab.RunsDir(path); *runs == "" {
			return fmt.Errorf("%w: the spec is not in lab/specs, so lab run needs -runs", errUsage)
		}
	}
	if *host == "" {
		*host, _ = os.Hostname()
	}
	commit, modified := toolInfo()
	rec, err := lab.Run(spec, lab.Options{Runs: *runs, Force: *force, Host: *host, ToolVersion: Version, Commit: commit, Modified: modified, Go: runtime.Version()})
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "wrote %s in %.1f s\n%s\n", filepath.Join(*runs, rec.ID), rec.WallSeconds, rec.Result)
	if rec.Mismatches > 0 {
		return fmt.Errorf("the run %s contradicts the prediction; see its summary.md", rec.ID)
	}
	return nil
}

func labSpec(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("mmp lab spec", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return usageErr(err)
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("%w: lab spec needs one experiment id", errUsage)
	}
	switch e := lab.FindExperiment(fs.Arg(0)); {
	case e == nil:
		return fmt.Errorf("%w: %q is not one of E1 to E10 of spec 13.3", errUsage, fs.Arg(0))
	case !e.Runnable:
		return fmt.Errorf("mmp has no code for experiment %s yet", e.ID)
	}
	_, err := io.WriteString(stdout, lab.SpecE2)
	return err
}

func labShow(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("mmp lab show", flag.ContinueOnError)
	fs.SetOutput(stderr)
	content := fs.String("content", ".", "the root of the content repository")
	if err := fs.Parse(args); err != nil {
		return usageErr(err)
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("%w: lab show needs one run id or run directory, after the flags", errUsage)
	}
	dir := fs.Arg(0)
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		dir = filepath.Join(*content, "lab", "runs", fs.Arg(0))
	}
	rec, err := lab.LoadRun(dir)
	if err != nil {
		return err
	}
	commit := rec.Commit
	if commit == "" {
		commit = "unknown"
	} else if rec.Modified {
		commit += " with changes"
	}
	fmt.Fprintf(stdout, "run:      %s\nspec:     %s (sha256 %.12s)\nnode:     %s\ntool:     %s %s, commit %s, %s\nhost:     %s, started %s, %.1f s\nseed:     %d\nresult:   %s\n", rec.ID, rec.Spec, rec.SpecSHA256, rec.Node, rec.Tool, rec.ToolVersion, commit, rec.Go, rec.Host, rec.Started, rec.WallSeconds, rec.Seed, rec.Result)
	summary, err := os.ReadFile(filepath.Join(dir, "summary.md")) // #nosec G304 -- a file of the content repository
	if err != nil {
		return err
	}
	fmt.Fprintf(stdout, "\n%s", summary)
	return nil
}
