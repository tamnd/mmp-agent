package main

import (
	"bufio"
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

	"go.yaml.in/yaml/v3"
)

// portEntry is one entry of manifests/ported.yaml in mmp-lean, spec 05.4.
type portEntry struct {
	Source  string   `yaml:"source"`
	Repo    string   `yaml:"repo"`
	Commit  string   `yaml:"commit"`
	Licence string   `yaml:"licence"`
	Date    string   `yaml:"date"`
	Paths   []string `yaml:"paths"`
	Into    string   `yaml:"into"` // a Lean file, or a directory of Lean files
	Changes string   `yaml:"changes"`
}

// portLicences are the licences that a ported source may have. Each one lets the project copy and change the files.
var portLicences = map[string]bool{"Apache-2.0": true, "MIT": true, "BSD-3-Clause": true}

var commitRE = regexp.MustCompile(`^[0-9a-f]{40}$`)
var dateRE = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

func runPort(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("%w: port needs a subcommand: check (add and diff come in milestone S1)", errUsage)
	}
	switch args[0] {
	case "check":
		fs := flag.NewFlagSet("mmp port check", flag.ContinueOnError)
		fs.SetOutput(stderr)
		lean := fs.String("lean", ".", "the root of mmp-lean")
		if err := parse(fs, args[1:]); err != nil {
			return err
		}
		probs, files, err := portCheck(*lean)
		if err != nil {
			return err
		}
		for _, s := range probs {
			fmt.Fprintln(stdout, "problem:", s)
		}
		if len(probs) > 0 {
			return fmt.Errorf("the ports have %d problems", len(probs))
		}
		fmt.Fprintf(stdout, "the ports are good: %d Lean files\n", files)
		return nil
	case "add", "list", "diff":
		return fmt.Errorf("port %s is not written yet. Milestone S1 adds it", args[0])
	}
	return fmt.Errorf("%w: unknown port subcommand %q", errUsage, args[0])
}

// portCheck checks manifests/ported.yaml against the files of MinimalModel/Ported. It returns the problems and the number of Lean files that it read.
//
// The checks are these. Each entry has a source, a repository, a commit of 40 hex digits, a permitted licence, a date, the source paths and a target. Each Lean file of the target starts with a licence header, and has a provenance line that names the repository and the commit. Each Lean file of MinimalModel/Ported belongs to exactly one entry.
func portCheck(root string) ([]string, int, error) {
	data, err := os.ReadFile(filepath.Join(root, "manifests", "ported.yaml")) // #nosec G304 -- a file of mmp-lean
	if err != nil {
		return nil, 0, err
	}
	var entries []portEntry
	if err := yaml.Unmarshal(data, &entries); err != nil {
		return nil, 0, fmt.Errorf("manifests/ported.yaml: %w", err)
	}
	var probs []string
	owner := map[string]string{} // Lean file → source
	files := 0
	seen := map[string]bool{}
	for i, e := range entries {
		name := e.Source
		if name == "" {
			name = fmt.Sprintf("entry %d", i+1)
			probs = append(probs, name+": no source")
		}
		if seen[e.Source] {
			probs = append(probs, name+": two entries have this source")
		}
		seen[e.Source] = true
		repo, ok := strings.CutPrefix(e.Repo, "https://github.com/")
		if !ok || strings.Count(repo, "/") != 1 {
			probs = append(probs, name+": the repo is not a GitHub URL: "+e.Repo)
		}
		if !commitRE.MatchString(e.Commit) {
			probs = append(probs, name+": the commit is not 40 hex digits: "+e.Commit)
		}
		if !portLicences[e.Licence] {
			probs = append(probs, name+": the licence "+e.Licence+" is not one of Apache-2.0, MIT and BSD-3-Clause")
		}
		if !dateRE.MatchString(e.Date) {
			probs = append(probs, name+": the date is not YYYY-MM-DD: "+e.Date)
		}
		if len(e.Paths) == 0 {
			probs = append(probs, name+": no source paths")
		}
		if !strings.HasPrefix(e.Into, "MinimalModel/Ported/") {
			probs = append(probs, name+": the target is not in MinimalModel/Ported: "+e.Into)
			continue
		}
		targets, err := leanFiles(filepath.Join(root, e.Into))
		if err != nil {
			probs = append(probs, name+": "+err.Error())
			continue
		}
		if len(targets) == 0 {
			probs = append(probs, name+": the target has no Lean files: "+e.Into)
		}
		for _, f := range targets {
			r := relPath(root, f)
			if prev, dup := owner[r]; dup {
				probs = append(probs, r+": in the entries "+prev+" and "+name)
				continue
			}
			owner[r] = name
			files++
			probs = append(probs, checkPortedFile(f, r, repo, e.Commit)...)
		}
	}
	all, err := leanFiles(filepath.Join(root, "MinimalModel", "Ported"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, 0, err
	}
	for _, f := range all {
		if r := relPath(root, f); owner[r] == "" {
			probs = append(probs, r+": in MinimalModel/Ported but in no entry of manifests/ported.yaml")
		}
	}
	sort.Strings(probs)
	return probs, files, nil
}

// checkPortedFile checks the licence header and the provenance line of one ported Lean file.
func checkPortedFile(path, rel, repo, commit string) []string {
	f, err := os.Open(path) // #nosec G304 -- a file of mmp-lean
	if err != nil {
		return []string{rel + ": " + err.Error()}
	}
	defer func() { _ = f.Close() }()
	var head []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() && len(head) < 40 {
		head = append(head, sc.Text())
	}
	var probs []string
	if len(head) == 0 || !strings.HasPrefix(head[0], "/-") {
		probs = append(probs, rel+": the file does not start with the licence header of its source")
	} else {
		text := strings.Join(head, "\n")
		end := strings.Index(text, "-/")
		if end < 0 || (!strings.Contains(text[:end], "Copyright") && !strings.Contains(text[:end], "License")) {
			probs = append(probs, rel+": the first comment is not a licence header")
		}
	}
	want := "Ported from " + repo + " at commit " + commit
	found := false
	for _, l := range head {
		if strings.Contains(l, want) {
			found = true
			break
		}
	}
	if !found {
		probs = append(probs, rel+": no provenance line \""+want+"\" in the first 40 lines")
	}
	return probs
}

// leanFiles returns the Lean files at path: the file itself, or the Lean files below the directory.
func leanFiles(path string) ([]string, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		return []string{path}, nil
	}
	var out []string
	err = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(p, ".lean") {
			out = append(out, p)
		}
		return nil
	})
	sort.Strings(out)
	return out, err
}

// relPath is path relative to root, with forward slashes.
func relPath(root, path string) string {
	r, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(r)
}
