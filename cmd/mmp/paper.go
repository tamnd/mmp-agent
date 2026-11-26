package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/tamnd/mmp-agent/blueprint"
	"go.yaml.in/yaml/v3"
)

func runPaper(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("%w: paper needs a subcommand: add, list or show", errUsage)
	}
	switch args[0] {
	case "add":
		return paperAdd(args[1:], stdout, stderr)
	case "list":
		return paperList(args[1:], stdout, stderr)
	case "show":
		return paperShow(args[1:], stdout, stderr)
	}
	return fmt.Errorf("%w: unknown paper subcommand %q; use add, list or show", errUsage, args[0])
}

var (
	arxivRE = regexp.MustCompile(`^([0-9]{4}\.[0-9]{4,5}|[a-z-]+(\.[A-Z]{2})?/[0-9]{7})(v[0-9]+)?$`)
	doiRE   = regexp.MustCompile(`^10\.[0-9]{4,9}/\S+$`)
)

// paperFile is a paper record as mmp writes it. The fields are in the order of spec 07.13, and a pointer field is null when it is not known.
type paperFile struct {
	ID            string                    `yaml:"id"`
	Key           string                    `yaml:"key"`
	Title         string                    `yaml:"title"`
	Authors       []string                  `yaml:"authors,flow"`
	Where         *string                   `yaml:"where"`
	Year          int                       `yaml:"year,omitempty"`
	DOI           *string                   `yaml:"doi"`
	Arxiv         *string                   `yaml:"arxiv"`
	Also          []string                  `yaml:"also,flow"`
	Read          paperRead                 `yaml:"read,flow"`
	Tier          string                    `yaml:"tier"`
	Why           string                    `yaml:"why"`
	ExpertStatus  string                    `yaml:"expert_status"`
	Uses          []string                  `yaml:"uses,flow"`
	Results       []blueprint.Result        `yaml:"results,omitempty"`
	LeanElsewhere []blueprint.LeanElsewhere `yaml:"lean_elsewhere,omitempty"`
	LeanStatus    string                    `yaml:"lean_status"`
	CodeLicence   *string                   `yaml:"code_licence"`
	Notes         string                    `yaml:"notes,omitempty"`
}

// paperRead is the read field. A field is null until a person or the reader role reads the paper.
type paperRead struct {
	Version *string `yaml:"version"`
	By      *string `yaml:"by"`
	Date    *string `yaml:"date"`
	Tag     string  `yaml:"tag"`
}

func orNull(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func paperAdd(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("mmp paper add", flag.ContinueOnError)
	fs.SetOutput(stderr)
	content := fs.String("content", ".", "the root of the content repository")
	key := fs.String("key", "", "the key of the paper, for example Kut80; the file is papers/<key>.yaml")
	id := fs.String("id", "", "the corpus id of spec 07, for example T5-10")
	title := fs.String("title", "", "the title")
	where := fs.String("where", "", "the journal or the book, with the pages")
	year := fs.Int("year", 0, "the year of the paper")
	doi := fs.String("doi", "", "the DOI")
	arxiv := fs.String("arxiv", "", "the arXiv id, with the version that was read, for example 2401.01234v2")
	tier := fs.String("tier", "", "the tier, T0 to T10 or S")
	why := fs.String("why", "", "why the project needs the paper")
	expert := fs.String("expert-status", "", "accepted, claimed, announced, withdrawn or disputed; a person sets it")
	leanStatus := fs.String("lean-status", "none", "none, stated, port-available or proved")
	licence := fs.String("code-licence", "", "the licence of the code that the project ports for this paper")
	readVersion := fs.String("read-version", "", "the version that was read, for example journal, book or v2 (default: the version of the arXiv id)")
	readBy := fs.String("read-by", "", "who read the paper")
	readDate := fs.String("read-date", "", "the date of the reading, YYYY-MM-DD")
	readTag := fs.String("read-tag", "U", "P (proved in the paper), S (stated with a reference) or U (not checked)")
	notes := fs.String("notes", "", "notes for the reader role")
	var authors, also, uses, results multiFlag
	fs.Var(&authors, "author", "an author; give the flag once for each author")
	fs.Var(&also, "also", "the corpus id of a related record; give the flag once for each")
	fs.Var(&uses, "uses", "the corpus id of a paper that this paper uses; give the flag once for each")
	fs.Var(&results, "result", `a numbered result as "id|statement|node|kind", for example "Theorem 2|LocalReciprocityGL1|MMAV7YV6K6|theorem"; the node can be empty`)
	if err := fs.Parse(args); err != nil {
		return usageErr(err)
	}
	switch rest := fs.Args(); {
	case len(rest) > 1:
		return fmt.Errorf("%w: paper add takes at most one arXiv id or DOI, after the flags", errUsage)
	case len(rest) == 1 && arxivRE.MatchString(strings.TrimPrefix(rest[0], "arXiv:")):
		if *arxiv != "" {
			return fmt.Errorf("%w: give the arXiv id once", errUsage)
		}
		*arxiv = strings.TrimPrefix(rest[0], "arXiv:")
	case len(rest) == 1 && doiRE.MatchString(rest[0]):
		if *doi != "" {
			return fmt.Errorf("%w: give the DOI once", errUsage)
		}
		*doi = rest[0]
	case len(rest) == 1:
		return fmt.Errorf("%w: %q is not an arXiv id or a DOI", errUsage, rest[0])
	}
	if *arxiv != "" && !arxivRE.MatchString(*arxiv) {
		return fmt.Errorf("%w: %q is not an arXiv id", errUsage, *arxiv)
	}
	if *doi != "" && !doiRE.MatchString(*doi) {
		return fmt.Errorf("%w: %q is not a DOI", errUsage, *doi)
	}
	var missing []string
	for _, f := range []struct{ name, value string }{{"-key", *key}, {"-id", *id}, {"-title", *title}, {"-tier", *tier}, {"-why", *why}, {"-expert-status", *expert}} {
		if strings.TrimSpace(f.value) == "" {
			missing = append(missing, f.name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("%w: paper add needs %s; mmp does not fetch the metadata yet", errUsage, strings.Join(missing, ", "))
	}
	if *readDate != "" && !dateRE.MatchString(*readDate) {
		return fmt.Errorf("%w: the date %q is not YYYY-MM-DD", errUsage, *readDate)
	}
	if *readTag != "U" && (*readBy == "" || *readDate == "") {
		return fmt.Errorf("%w: the read tag %s needs -read-by and -read-date; use the tag U for a paper that nobody checked", errUsage, *readTag)
	}
	if *readVersion == "" {
		if m := arxivRE.FindStringSubmatch(*arxiv); m != nil && m[3] != "" {
			*readVersion = m[3]
		}
	}
	p, err := blueprint.Load(*content)
	if err != nil {
		return err
	}
	rec := paperFile{
		ID: *id, Key: *key, Title: *title, Authors: authors, Where: orNull(*where), Year: *year,
		DOI: orNull(*doi), Arxiv: orNull(*arxiv), Also: also, Tier: *tier, Why: *why, ExpertStatus: *expert, Uses: uses,
		Read:       paperRead{Version: orNull(*readVersion), By: orNull(*readBy), Date: orNull(*readDate), Tag: *readTag},
		LeanStatus: *leanStatus, CodeLicence: orNull(*licence), Notes: *notes,
	}
	if rec.Also == nil {
		rec.Also = []string{}
	}
	if rec.Uses == nil {
		rec.Uses = []string{}
	}
	for _, r := range results {
		parts := strings.Split(r, "|")
		if len(parts) != 4 {
			return fmt.Errorf("%w: the result %q is not \"id|statement|node|kind\"", errUsage, r)
		}
		res := blueprint.Result{ID: parts[0], Statement: parts[1], Node: parts[2], Kind: parts[3], Status: blueprint.Planned}
		if n := p.Node(res.Node); n != nil {
			res.Status = n.Status
		}
		rec.Results = append(rec.Results, res)
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(rec); err != nil {
		return err
	}
	if err := enc.Close(); err != nil {
		return err
	}
	out := buf.Bytes()
	// Decode the text as the loader does, so that mmp never writes a record that mmp check cannot read.
	var pp blueprint.Paper
	dec := yaml.NewDecoder(bytes.NewReader(out))
	dec.KnownFields(true)
	if err := dec.Decode(&pp); err != nil {
		return fmt.Errorf("the new record does not decode: %w", err)
	}
	file := "papers/" + *key + ".yaml"
	pp.File = file
	if old := p.Paper(*key); old != nil {
		return fmt.Errorf("the key %s is already in %s", *key, old.File)
	}
	p.Papers = append(p.Papers, &pp)
	var probs []string
	for _, line := range p.Check() {
		if strings.HasPrefix(line, file+": ") {
			probs = append(probs, line)
		}
	}
	if len(probs) > 0 {
		for _, line := range probs {
			fmt.Fprintln(stderr, line)
		}
		return fmt.Errorf("the record has %s; nothing was written", plural(len(probs), "problem"))
	}
	dst := filepath.Join(*content, filepath.FromSlash(file))
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return err
	}
	f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644) // #nosec G302 G304 -- a public record of the content repository
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return fmt.Errorf("%s exists; mmp paper add does not overwrite a record", file)
		}
		return err
	}
	if _, err := f.Write(out); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	fmt.Fprintf(stdout, "wrote %s\n", dst)
	return nil
}

func paperList(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("mmp paper list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	content := fs.String("content", ".", "the root of the content repository")
	tier := fs.String("tier", "", "list only the papers of this tier")
	if err := parse(fs, args); err != nil {
		return err
	}
	if *tier != "" && !contains(blueprint.Tiers, *tier) {
		return fmt.Errorf("%w: the tier %q is not one of %s", errUsage, *tier, strings.Join(blueprint.Tiers, ", "))
	}
	p, err := blueprint.Load(*content)
	if err != nil {
		return err
	}
	n := 0
	for _, pp := range p.Papers {
		if *tier != "" && pp.Tier != *tier {
			continue
		}
		n++
		fmt.Fprintf(stdout, "%-8s %-6s %-4s %-9s %-14s %-8s %s\n", pp.Key, pp.ID, pp.Tier, pp.ExpertStatus, pp.LeanStatus, plural(len(pp.Nodes), "node"), pp.Title)
	}
	fmt.Fprintln(stdout, plural(n, "paper"))
	return nil
}

func paperShow(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("mmp paper show", flag.ContinueOnError)
	fs.SetOutput(stderr)
	content := fs.String("content", ".", "the root of the content repository")
	if err := fs.Parse(args); err != nil {
		return usageErr(err)
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("%w: paper show needs one key, after the flags", errUsage)
	}
	p, err := blueprint.Load(*content)
	if err != nil {
		return err
	}
	pp := p.Paper(fs.Arg(0))
	if pp == nil {
		return fmt.Errorf("there is no paper with the key %s", fs.Arg(0))
	}
	field := func(name, value string) {
		if value != "" {
			fmt.Fprintf(stdout, "%-14s %s\n", name+":", value)
		}
	}
	field("key", pp.Key)
	field("id", pp.ID)
	field("title", pp.Title)
	field("authors", strings.Join(pp.Authors, ", "))
	field("where", pp.Where)
	if pp.Year != 0 {
		field("year", strconv.Itoa(pp.Year))
	}
	field("doi", pp.DOI)
	field("arxiv", pp.Arxiv)
	field("tier", pp.Tier)
	field("why", pp.Why)
	field("expert status", pp.ExpertStatus)
	field("lean status", pp.LeanStatus)
	field("code licence", pp.CodeLicence)
	r := pp.Read
	field("read", strings.TrimSpace(strings.Join([]string{r.Version, r.By, r.Date, "tag " + r.Tag}, " ")))
	field("also", strings.Join(pp.Also, ", "))
	field("uses", strings.Join(pp.Uses, ", "))
	field("nodes", strings.Join(pp.Nodes, ", "))
	for _, res := range pp.Results {
		status := blueprint.Planned
		if n := p.Node(res.Node); n != nil {
			status = n.Status
		}
		line := fmt.Sprintf("result %s: %s (%s), %s", res.ID, res.Statement, res.Kind, status)
		switch {
		case res.Node != "" && p.Node(res.Node) == nil:
			line += "; the node " + res.Node + " is not in the map"
		case res.Node != "":
			line += " in " + res.Node
		}
		if res.Status != status {
			line += fmt.Sprintf("; the record says %s, but the map says %s", res.Status, status)
		}
		fmt.Fprintln(stdout, line)
	}
	for _, l := range pp.LeanElsewhere {
		fmt.Fprintf(stdout, "lean elsewhere: %s (%s, %s): %s\n", l.Repo, l.Licence, l.Use, l.What)
	}
	field("notes", pp.Notes)
	return nil
}
