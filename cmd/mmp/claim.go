package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/tamnd/mmp-agent/blueprint"
	"go.yaml.in/yaml/v3"
)

// Claim statuses. A record starts as fresh. The reader role and a person move it on. A record is never removed: a claim that its authors withdraw keeps its record with the status withdrawn.
var claimStatuses = []string{
	"fresh",     // the record exists, and nobody has read the claim
	"reading",   // the reader role reads the claim and proposes the self-contained parts
	"planned",   // a person accepted at least one part
	"checking",  // a role works on a part
	"done",      // each accepted part is done or has a gap, and the letters are sent (spec 14.4)
	"withdrawn", // the authors withdrew the claim
}

// Part statuses. "done" means that the Lean check of the part passed, and "gap" means that the check found a step that does not follow as stated (spec 12.3).
var partStatuses = []string{"proposed", "accepted", "stated", "done", "gap", "dropped"}

var (
	partIDRE   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]*$`)
	arxivURLRE = regexp.MustCompile(`^/(abs|pdf)/(.+?)(\.pdf)?$`)
	claimKeyRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9-]*$`)
)

// claimFile is a claim record, claims/<key>.yaml, of route R1 (spec 04.2, 07.12 and 08.4). A pointer field is null when it is not known.
type claimFile struct {
	Key     string        `yaml:"key"`
	Title   *string       `yaml:"title"`
	Authors []string      `yaml:"authors,flow"`
	Source  claimSource   `yaml:"source,flow"`
	Date    string        `yaml:"date"`   // the date of the record
	Posted  *string       `yaml:"posted"` // the date of the preprint, when it is known
	Status  string        `yaml:"status"`
	Route   string        `yaml:"route"`
	Claims  *string       `yaml:"claims"` // what the preprint claims, in plain words
	Node    *string       `yaml:"node"`   // the node of kind claim that has the main statement, when there is one
	Parts   []claimPart   `yaml:"parts"`
	Letters []claimLetter `yaml:"letters"`
	Notes   string        `yaml:"notes,omitempty"`
}

// claimSource is the preprint of a claim: an arXiv id or an address. Version is the version that the record reads.
type claimSource struct {
	Arxiv   *string `yaml:"arxiv"`
	URL     *string `yaml:"url"`
	Version *string `yaml:"version"`
}

// claimPart is a self-contained part of a claim: a part that is small enough to check in Lean and that uses only other parts, finished nodes and named hypotheses (spec 04.2, 10.3).
type claimPart struct {
	ID         string   `yaml:"id"`
	Title      string   `yaml:"title"`
	Where      string   `yaml:"where"`                     // the place in the preprint, for example "Section 3, Theorem 3.4"
	Statement  string   `yaml:"statement"`                 // the statement of the part, in plain words and math
	Lean       string   `yaml:"lean,omitempty"`            // the Lean statement of the part
	Node       string   `yaml:"node,omitempty"`            // the node of the map, when there is one
	Uses       []string `yaml:"uses,flow,omitempty"`       // the other parts and the nodes that the part uses
	Hypotheses []string `yaml:"hypotheses,flow,omitempty"` // the named hypotheses for the foundations that Lean does not have
	Cost       int      `yaml:"cost"`                      // the cost of the formal check, 1 (cheap) to 5 (out of reach), as in spec 04
	Status     string   `yaml:"status"`
	Result     string   `yaml:"result,omitempty"` // what the check found, in neutral words (spec 12.3)
}

// claimLetter is a letter to the authors (spec 14.4).
type claimLetter struct {
	To         string `yaml:"to"`
	Date       string `yaml:"date"`
	About      string `yaml:"about"`
	Answer     string `yaml:"answer,omitempty"`
	AnswerDate string `yaml:"answer_date,omitempty"`
}

// claimEntry is a claim record and its file, relative to the content repository.
type claimEntry struct {
	File   string
	Record claimFile
}

func runClaim(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("%w: claim needs a subcommand: add, list, show or plan", errUsage)
	}
	switch args[0] {
	case "add":
		return claimAdd(args[1:], stdout, stderr)
	case "list":
		return claimList(args[1:], stdout, stderr)
	case "show":
		return claimShow(args[1:], stdout, stderr)
	case "plan":
		return claimPlan(args[1:], stdout, stderr)
	}
	return fmt.Errorf("%w: unknown claim subcommand %q; use add, list, show or plan", errUsage, args[0])
}

// claimSourceOf reads the argument of claim add: an arXiv id, with or without "arXiv:", an address of arxiv.org, or another http or https address. It returns the arXiv id without its version and the version, or the address.
func claimSourceOf(arg string) (arxiv, version, address string, err error) {
	arg = strings.TrimSpace(arg)
	id := strings.TrimPrefix(arg, "arXiv:")
	if u, perr := url.Parse(arg); perr == nil && (u.Scheme == "http" || u.Scheme == "https") {
		if u.Host == "" {
			return "", "", "", fmt.Errorf("%w: the address %q has no host", errUsage, arg)
		}
		host := strings.TrimPrefix(strings.ToLower(u.Host), "www.")
		m := arxivURLRE.FindStringSubmatch(u.Path)
		if host != "arxiv.org" || m == nil || !arxivRE.MatchString(m[2]) {
			u.Scheme, u.Host, u.Fragment = strings.ToLower(u.Scheme), strings.ToLower(u.Host), ""
			u.Path = strings.TrimSuffix(u.Path, "/")
			return "", "", u.String(), nil
		}
		id = m[2]
	}
	sub := arxivRE.FindStringSubmatch(id)
	if sub == nil {
		return "", "", "", fmt.Errorf("%w: %q is not an arXiv id or an http or https address", errUsage, arg)
	}
	return strings.TrimSuffix(id, sub[3]), sub[3], "", nil
}

// claimKey is the stable key of a source. The same preprint gives the same key, whatever its version: arXiv:2611.31396v2 gives arXiv-2611-31396. An address gives url- and the first 10 hex digits of its sha256.
func claimKey(arxiv, address string) string {
	if arxiv != "" {
		return "arXiv-" + strings.NewReplacer(".", "-", "/", "-").Replace(arxiv)
	}
	sum := sha256.Sum256([]byte(address))
	return "url-" + hex.EncodeToString(sum[:])[:10]
}

// loadClaims reads the claim records of claims/, sorted by file name. A missing directory has no records.
func loadClaims(content string) ([]*claimEntry, error) {
	dir := filepath.Join(content, "claims")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []*claimEntry
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name())) // #nosec G304 -- a file of the content repository
		if err != nil {
			return nil, err
		}
		c, err := decodeClaim(data)
		if err != nil {
			return nil, fmt.Errorf("claims/%s: %w", e.Name(), err)
		}
		out = append(out, &claimEntry{File: "claims/" + e.Name(), Record: c})
	}
	return out, nil
}

func decodeClaim(data []byte) (claimFile, error) {
	var c claimFile
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	err := dec.Decode(&c)
	return c, err
}

func findClaim(list []*claimEntry, key string) *claimEntry {
	for _, c := range list {
		if c.Record.Key == key {
			return c
		}
	}
	return nil
}

func validDate(s string) bool {
	if !dateRE.MatchString(s) {
		return false
	}
	_, err := time.Parse("2006-01-02", s)
	return err == nil
}

// claimProblems checks a claim record against the map. mmp check, mmp claim add and mmp claim plan use it.
func claimProblems(p *blueprint.Project, e *claimEntry) []string {
	var out []string
	bad := func(format string, args ...any) {
		out = append(out, e.File+": "+fmt.Sprintf(format, args...))
	}
	c := e.Record
	if !claimKeyRE.MatchString(c.Key) {
		bad("the key %q has a character that is not a letter, a digit or a hyphen, or it does not start with a letter", c.Key)
	}
	if want := c.Key + ".yaml"; filepath.Base(e.File) != want {
		bad("the file name must be %s", want)
	}
	arxiv, address := deref(c.Source.Arxiv), deref(c.Source.URL)
	switch {
	case arxiv == "" && address == "":
		bad("the source has no arXiv id and no address")
	case arxiv != "" && address != "":
		bad("the source has an arXiv id and an address; give one")
	case arxiv != "" && !arxivRE.MatchString(arxiv):
		bad("the arXiv id %q is not an arXiv id", arxiv)
	case address != "":
		if u, err := url.Parse(address); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			bad("the address %q is not an http or https address", address)
		}
	}
	if !validDate(c.Date) {
		bad("the date %q is not a date YYYY-MM-DD", c.Date)
	}
	if c.Posted != nil && !validDate(*c.Posted) {
		bad("the posted date %q is not a date YYYY-MM-DD", *c.Posted)
	}
	if !contains(claimStatuses, c.Status) {
		bad("the status %q is not one of %s", c.Status, strings.Join(claimStatuses, ", "))
	}
	if c.Route != "R1" {
		bad("the route is %q; a claim record belongs to route R1", c.Route)
	}
	if n := deref(c.Node); n != "" {
		switch m := p.Node(n); {
		case m == nil:
			bad("the node %s is not in the map", n)
		case m.Kind != "claim":
			bad("the node %s has the kind %s; the node of a claim has the kind claim", n, m.Kind)
		}
	}
	ids := map[string]bool{}
	for _, part := range c.Parts {
		ids[part.ID] = true
	}
	seen := map[string]bool{}
	for i, part := range c.Parts {
		name := part.ID
		if name == "" {
			name = fmt.Sprintf("number %d", i+1)
		}
		switch {
		case !partIDRE.MatchString(part.ID):
			bad("the part %s has no id, or its id has a space or a sign", name)
		case seen[part.ID]:
			bad("the part id %s is used twice", part.ID)
		}
		seen[part.ID] = true
		if strings.TrimSpace(part.Title) == "" || strings.TrimSpace(part.Statement) == "" {
			bad("the part %s needs a title and a statement", name)
		}
		if strings.TrimSpace(part.Where) == "" {
			bad("the part %s needs the place in the preprint (where)", name)
		}
		if part.Cost < 1 || part.Cost > 5 {
			bad("the part %s has the cost %d; the cost is 1 to 5", name, part.Cost)
		}
		if !contains(partStatuses, part.Status) {
			bad("the part %s has the status %q, which is not one of %s", name, part.Status, strings.Join(partStatuses, ", "))
		}
		if (part.Status == "stated" || part.Status == "done" || part.Status == "gap") && part.Lean == "" {
			bad("the part %s has the status %s but no Lean statement", name, part.Status)
		}
		if (part.Status == "done" || part.Status == "gap") && strings.TrimSpace(part.Result) == "" {
			bad("the part %s has the status %s but no result", name, part.Status)
		}
		if part.Node != "" && p.Node(part.Node) == nil {
			bad("the part %s names the node %s, which is not in the map", name, part.Node)
		}
		for _, u := range part.Uses {
			switch {
			case u == part.ID:
				bad("the part %s uses itself", name)
			case ids[u]:
			case p.Node(u) == nil:
				bad("the part %s uses %s, which is not a part of the record and not a node", name, u)
			}
		}
	}
	for i, l := range c.Letters {
		if strings.TrimSpace(l.To) == "" || strings.TrimSpace(l.About) == "" {
			bad("the letter %d needs to and about", i+1)
		}
		if !validDate(l.Date) {
			bad("the letter %d has the date %q, which is not YYYY-MM-DD", i+1, l.Date)
		}
		if l.AnswerDate != "" && !validDate(l.AnswerDate) {
			bad("the letter %d has the answer date %q, which is not YYYY-MM-DD", i+1, l.AnswerDate)
		}
	}
	return out
}

// allClaimProblems checks each record and the keys and sources that two records share.
func allClaimProblems(p *blueprint.Project, list []*claimEntry) []string {
	var out []string
	keys := map[string]string{}
	sources := map[string]string{}
	for _, e := range list {
		out = append(out, claimProblems(p, e)...)
		if f, dup := keys[e.Record.Key]; dup {
			out = append(out, fmt.Sprintf("%s: the key %s is also in %s", e.File, e.Record.Key, f))
		}
		keys[e.Record.Key] = e.File
		src := sourceText(e.Record.Source)
		if f, dup := sources[src]; dup && src != "" {
			out = append(out, fmt.Sprintf("%s: the source %s is also in %s", e.File, src, f))
		}
		sources[src] = e.File
	}
	return out
}

// sourceText is the source of a claim with no version, for messages and for the search of a duplicate.
func sourceText(s claimSource) string {
	if a := deref(s.Arxiv); a != "" {
		if m := arxivRE.FindStringSubmatch(a); m != nil {
			a = strings.TrimSuffix(a, m[3])
		}
		return "arXiv:" + a
	}
	return deref(s.URL)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func claimAdd(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("mmp claim add", flag.ContinueOnError)
	fs.SetOutput(stderr)
	content := fs.String("content", ".", "the root of the content repository")
	key := fs.String("key", "", "the key of the record (default: a stable key from the source, for example arXiv-2611-31396)")
	date := fs.String("date", "", "the date of the record, YYYY-MM-DD (default: today, UTC)")
	title := fs.String("title", "", "the title of the preprint")
	posted := fs.String("posted", "", "the date of the preprint, YYYY-MM-DD")
	var authors multiFlag
	fs.Var(&authors, "author", "an author; give the flag once for each author")
	fs.Usage = func() {
		fmt.Fprintln(stderr, "Usage: mmp claim add [flags] <arXiv id | URL>")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return usageErr(err)
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("%w: claim add needs one arXiv id or one address, after the flags", errUsage)
	}
	arxiv, version, address, err := claimSourceOf(fs.Arg(0))
	if err != nil {
		return err
	}
	if *date == "" {
		*date = time.Now().UTC().Format("2006-01-02")
	}
	if !validDate(*date) {
		return fmt.Errorf("%w: the date %q is not YYYY-MM-DD", errUsage, *date)
	}
	if *posted != "" && !validDate(*posted) {
		return fmt.Errorf("%w: the posted date %q is not YYYY-MM-DD", errUsage, *posted)
	}
	if *key == "" {
		*key = claimKey(arxiv, address)
	}
	if _, err := os.Stat(filepath.Join(*content, "blueprint")); err != nil {
		return fmt.Errorf("%s is not the content repository: %w", *content, err)
	}
	p, err := blueprint.Load(*content)
	if err != nil {
		return err
	}
	list, err := loadClaims(*content)
	if err != nil {
		return err
	}
	rec := claimFile{
		Key: *key, Title: orNull(*title), Authors: authors, Date: *date, Posted: orNull(*posted),
		Source: claimSource{Arxiv: orNull(arxiv), URL: orNull(address), Version: orNull(version)},
		Status: "fresh", Route: "R1", Parts: []claimPart{}, Letters: []claimLetter{},
	}
	if rec.Authors == nil {
		rec.Authors = []string{}
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
	// Decode the text as the loader does, so that mmp never writes a record that it cannot read.
	back, err := decodeClaim(out)
	if err != nil {
		return fmt.Errorf("the new record does not decode: %w", err)
	}
	file := "claims/" + *key + ".yaml"
	entry := &claimEntry{File: file, Record: back}
	if old := findClaim(list, *key); old != nil {
		return fmt.Errorf("the key %s is already in %s", *key, old.File)
	}
	src := sourceText(back.Source)
	for _, old := range list {
		if sourceText(old.Record.Source) == src {
			return fmt.Errorf("the source %s is already in %s", src, old.File)
		}
	}
	if probs := claimProblems(p, entry); len(probs) > 0 {
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
			return fmt.Errorf("%s exists; mmp claim add does not overwrite a record", file)
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
	fmt.Fprintf(stdout, "wrote %s: a fresh claim of route R1 with no parts; mmp did not fetch the preprint\n", dst)
	return nil
}

func claimList(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("mmp claim list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	content := fs.String("content", ".", "the root of the content repository")
	status := fs.String("status", "", "list only the claims with this status")
	if err := parse(fs, args); err != nil {
		return err
	}
	if *status != "" && !contains(claimStatuses, *status) {
		return fmt.Errorf("%w: the status %q is not one of %s", errUsage, *status, strings.Join(claimStatuses, ", "))
	}
	list, err := loadClaims(*content)
	if err != nil {
		return err
	}
	n := 0
	for _, e := range list {
		c := e.Record
		if *status != "" && c.Status != *status {
			continue
		}
		n++
		line := fmt.Sprintf("%-18s %-9s %s  %-8s %-22s %s", c.Key, c.Status, c.Date, plural(len(c.Parts), "part"), sourceText(c.Source), deref(c.Title))
		fmt.Fprintln(stdout, strings.TrimRight(line, " "))
	}
	fmt.Fprintln(stdout, plural(n, "claim"))
	return nil
}

// claimArg parses the flags and the one key of claim show and claim plan, and returns the record.
func claimArg(name string, args []string, stderr io.Writer) (*blueprint.Project, *claimEntry, []*claimEntry, error) {
	fs := flag.NewFlagSet("mmp claim "+name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	content := fs.String("content", ".", "the root of the content repository")
	if err := fs.Parse(args); err != nil {
		return nil, nil, nil, usageErr(err)
	}
	if fs.NArg() != 1 {
		return nil, nil, nil, fmt.Errorf("%w: claim %s needs one key, after the flags", errUsage, name)
	}
	p, err := blueprint.Load(*content)
	if err != nil {
		return nil, nil, nil, err
	}
	list, err := loadClaims(*content)
	if err != nil {
		return nil, nil, nil, err
	}
	e := findClaim(list, fs.Arg(0))
	if e == nil {
		return nil, nil, nil, fmt.Errorf("there is no claim with the key %s in claims/", fs.Arg(0))
	}
	return p, e, list, nil
}

func claimShow(args []string, stdout, stderr io.Writer) error {
	p, e, _, err := claimArg("show", args, stderr)
	if err != nil {
		return err
	}
	c := e.Record
	field := func(name, value string) {
		if value != "" {
			fmt.Fprintf(stdout, "%-10s %s\n", name+":", value)
		}
	}
	field("key", c.Key)
	field("file", e.File)
	field("title", deref(c.Title))
	field("authors", strings.Join(c.Authors, ", "))
	field("source", sourceText(c.Source))
	field("version", deref(c.Source.Version))
	field("posted", deref(c.Posted))
	field("date", c.Date)
	field("status", c.Status)
	field("route", c.Route)
	field("claims", deref(c.Claims))
	if n := deref(c.Node); n != "" {
		status := "not in the map"
		if m := p.Node(n); m != nil {
			status = m.Status
		}
		field("node", n+" ("+status+")")
	}
	if len(c.Parts) == 0 {
		fmt.Fprintln(stdout, "parts:     none yet; the reader role proposes them and a person accepts them (spec 10.3)")
	}
	for _, part := range c.Parts {
		fmt.Fprintf(stdout, "part %s: %s (%s, cost %d, %s)\n", part.ID, part.Title, part.Where, part.Cost, part.Status)
	}
	for _, l := range c.Letters {
		line := fmt.Sprintf("letter %s to %s about %s", l.Date, l.To, l.About)
		if l.Answer != "" {
			line += fmt.Sprintf("; answer %s: %s", l.AnswerDate, l.Answer)
		} else {
			line += "; no answer yet"
		}
		fmt.Fprintln(stdout, line)
	}
	field("notes", c.Notes)
	return nil
}

// selfContained reports whether each use of the part is another part, or a node that is proved or defined. It returns the uses that are not.
func selfContained(p *blueprint.Project, c claimFile, part claimPart) []string {
	ids := map[string]bool{}
	for _, q := range c.Parts {
		ids[q.ID] = true
	}
	var open []string
	for _, u := range part.Uses {
		if ids[u] {
			continue
		}
		if n := p.Node(u); n == nil || (n.Status != blueprint.Proved && n.Status != blueprint.Defined) {
			status := "not in the map"
			if n != nil {
				status = n.Status
			}
			open = append(open, u+" ("+status+")")
		}
	}
	return open
}

func claimPlan(args []string, stdout, stderr io.Writer) error {
	p, e, list, err := claimArg("plan", args, stderr)
	if err != nil {
		return err
	}
	var probs []string
	for _, line := range allClaimProblems(p, list) {
		if strings.HasPrefix(line, e.File+": ") {
			probs = append(probs, line)
		}
	}
	if len(probs) > 0 {
		for _, line := range probs {
			fmt.Fprintln(stdout, "problem:", line)
		}
		return fmt.Errorf("the record %s has %s", e.File, plural(len(probs), "problem"))
	}
	c := e.Record
	head := fmt.Sprintf("claim %s: %s, status %s", c.Key, sourceText(c.Source), c.Status)
	if v := deref(c.Source.Version); v != "" {
		head += ", read version " + v
	}
	fmt.Fprintln(stdout, head)
	if len(c.Parts) == 0 {
		fmt.Fprintln(stdout, "The record has no parts yet. The reader role proposes the self-contained parts, and a person accepts each one (spec 10.3).")
		return nil
	}
	cost, contained, done, gaps := 0, 0, 0, 0
	for _, part := range c.Parts {
		open := selfContained(p, c, part)
		state := "self-contained"
		if len(open) > 0 {
			state = "not self-contained: it uses " + strings.Join(open, ", ")
		} else {
			contained++
		}
		if part.Status != "dropped" {
			cost += part.Cost
		}
		switch part.Status {
		case "done":
			done++
		case "gap":
			gaps++
		}
		fmt.Fprintf(stdout, "\n%s  %s\n", part.ID, part.Title)
		fmt.Fprintf(stdout, "  cost:       %d of 5\n  status:     %s\n  where:      %s\n  statement:  %s\n", part.Cost, part.Status, part.Where, oneLine(part.Statement))
		lean := part.Lean
		if lean == "" {
			lean = "none yet; the stater role writes it"
		} else if _, ok := leanDecl(p, lean); !ok {
			lean += " (not in records/lean.json)"
		}
		fmt.Fprintf(stdout, "  lean:       %s\n", lean)
		if part.Node != "" {
			fmt.Fprintf(stdout, "  node:       %s\n", part.Node)
		}
		if len(part.Uses) > 0 {
			fmt.Fprintf(stdout, "  uses:       %s\n", strings.Join(part.Uses, ", "))
		}
		if len(part.Hypotheses) > 0 {
			fmt.Fprintf(stdout, "  hypotheses: %s\n", strings.Join(part.Hypotheses, ", "))
		}
		fmt.Fprintf(stdout, "  check:      %s\n", state)
		if part.Result != "" {
			fmt.Fprintf(stdout, "  result:     %s\n", oneLine(part.Result))
		}
	}
	fmt.Fprintf(stdout, "\n%s with a total cost of %d (dropped parts excluded); %d self-contained, %d done, %s\n", plural(len(c.Parts), "part"), cost, contained, done, plural(gaps, "gap"))
	return nil
}

func leanDecl(p *blueprint.Project, name string) (blueprint.Decl, bool) {
	if p.Lean == nil {
		return blueprint.Decl{}, false
	}
	d, ok := p.Lean.Decls[name]
	return d, ok
}

// oneLine joins the lines of a text with spaces.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
