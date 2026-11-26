package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tamnd/mmp-agent/blueprint"
	"github.com/tamnd/mmp-agent/lab"
)

var weekRE = regexp.MustCompile(`^([0-9]{4})-W([0-9]{2})$`)

// weekDays returns the Monday and the Sunday of an ISO week such as 2026-W49.
func weekDays(week string) (time.Time, time.Time, error) {
	m := weekRE.FindStringSubmatch(week)
	if m == nil {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: the week %q is not YYYY-Wnn", errUsage, week)
	}
	year, _ := strconv.Atoi(m[1])
	w, _ := strconv.Atoi(m[2])
	// Week 1 is the week with 4 January in it.
	jan4 := time.Date(year, time.January, 4, 0, 0, 0, 0, time.UTC)
	monday := jan4.AddDate(0, 0, -((int(jan4.Weekday())+6)%7)+7*(w-1))
	if y, ww := monday.ISOWeek(); w < 1 || y != year || ww != w {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: the year %d has no week %d", errUsage, year, w)
	}
	return monday, monday.AddDate(0, 0, 6), nil
}

func isoWeek(t time.Time) string {
	y, w := t.ISOWeek()
	return fmt.Sprintf("%d-W%02d", y, w)
}

// statusSnapshot is records/status/<week>.json: the status of each node on the date of the report. The next report compares with it.
type statusSnapshot struct {
	Week  string                  `json:"week"`
	Date  string                  `json:"date"`
	Nodes map[string]snapshotNode `json:"nodes"`
}

type snapshotNode struct {
	Status string   `json:"status"`
	Tier   string   `json:"tier"`
	Route  []string `json:"route,omitempty"`
}

// lastSnapshot reads the latest snapshot of a week before the week. It returns nil when there is none.
func lastSnapshot(content, week string) (*statusSnapshot, error) {
	dir := filepath.Join(content, "records", "status")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	best := ""
	for _, e := range entries {
		name := strings.TrimSuffix(e.Name(), ".json")
		if e.IsDir() || name == e.Name() || !weekRE.MatchString(name) || name >= week {
			continue
		}
		if name > best {
			best = name
		}
	}
	if best == "" {
		return nil, nil
	}
	path := filepath.Join(dir, best+".json")
	data, err := os.ReadFile(path) // #nosec G304 -- a record of the content repository
	if err != nil {
		return nil, err
	}
	var s statusSnapshot
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &s, nil
}

// plain makes text from the content repository fit the report: one line, and no em dash or en dash.
func plain(s string) string {
	s = strings.ReplaceAll(s, "—", ", ")
	s = strings.ReplaceAll(s, "–", "-")
	return oneLine(s)
}

// users returns the number of the nodes that use the node, directly or through other nodes.
func users(p *blueprint.Project, n *blueprint.Node) int {
	seen := map[string]bool{}
	var visit func(m *blueprint.Node)
	visit = func(m *blueprint.Node) {
		for _, u := range m.UsedBy {
			if seen[u] {
				continue
			}
			seen[u] = true
			if k := p.Node(u); k != nil {
				visit(k)
			}
		}
	}
	visit(n)
	delete(seen, n.Tag)
	return len(seen)
}

// blockers says what blocks a node that is not done.
func blockers(p *blueprint.Project, n *blueprint.Node) string {
	var open []string
	for _, u := range n.Uses {
		if m := p.Node(u); m == nil || !done(m) {
			open = append(open, u)
		}
	}
	if len(open) > 0 {
		return "it waits for " + strings.Join(open, ", ")
	}
	switch n.Status {
	case blueprint.Planned:
		return "it has no Lean statement"
	case blueprint.Open:
		return "it is an open problem"
	case blueprint.Claimed:
		return "it is a fresh claim with no check"
	}
	return "it has no proof yet"
}

func mismatches(n int) string {
	if n == 1 {
		return "1 mismatch"
	}
	return fmt.Sprintf("%d mismatches", n)
}

func inWeek(date string, from, to time.Time) bool {
	if len(date) < 10 {
		return false
	}
	d, err := time.Parse("2006-01-02", date[:10])
	return err == nil && !d.Before(from) && !d.After(to)
}

func agentReport(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("mmp agent report", flag.ContinueOnError)
	fs.SetOutput(stderr)
	content := fs.String("content", ".", "the root of the content repository")
	week := fs.String("week", "", "the ISO week, for example 2026-W49 (default: the week of today, UTC)")
	if err := parse(fs, args); err != nil {
		return err
	}
	if *week == "" {
		*week = isoWeek(time.Now().UTC())
	}
	from, to, err := weekDays(*week)
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(*content, "blueprint")); err != nil {
		return fmt.Errorf("%s is not the content repository: %w", *content, err)
	}
	p, err := blueprint.Load(*content)
	if err != nil {
		return err
	}
	claims, err := loadClaims(*content)
	if err != nil {
		return err
	}
	runs, err := loadRuns(*content)
	if err != nil {
		return err
	}
	ledger, err := loadLedger(*content, runs)
	if err != nil {
		return err
	}
	labRuns, err := lab.LoadRuns(filepath.Join(*content, "lab", "runs"))
	if err != nil {
		return err
	}
	prev, err := lastSnapshot(*content, *week)
	if err != nil {
		return err
	}
	today := time.Now().UTC().Format("2006-01-02")
	snap := statusSnapshot{Week: *week, Date: today, Nodes: map[string]snapshotNode{}}
	for _, n := range p.Nodes {
		snap.Nodes[n.Tag] = snapshotNode{Status: n.Status, Tier: n.Tier, Route: n.Route}
	}
	text := reportText(p, *week, from, to, today, prev, claims, runs, ledger, labRuns)

	dst := filepath.Join(*content, "reports", *week+".md")
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return err
	}
	if err := os.WriteFile(dst, []byte(text), 0o644); err != nil { // #nosec G306 -- a public report
		return err
	}
	snapPath := filepath.Join(*content, "records", "status", *week+".json")
	if err := os.MkdirAll(filepath.Dir(snapPath), 0o750); err != nil {
		return err
	}
	out, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(snapPath, append(out, '\n'), 0o644); err != nil { // #nosec G306 -- a public record
		return err
	}
	fmt.Fprintf(stdout, "wrote %s: the report of %s, from %s to %s\n", dst, *week, from.Format("2006-01-02"), to.Format("2006-01-02"))
	fmt.Fprintf(stdout, "wrote %s: the status of %s, for the next report\n", snapPath, plural(len(p.Nodes), "node"))
	return nil
}

// reportText writes the report of spec 10.8. Each paragraph is one line.
func reportText(p *blueprint.Project, week string, from, to time.Time, today string, prev *statusSnapshot, claims []*claimEntry, runs []*runRecord, ledger []ledgerEntry, labRuns []*lab.Record) string {
	var b strings.Builder
	day := func(t time.Time) string { return t.Format("2006-01-02") }
	fmt.Fprintf(&b, "# The weekly report of %s\n\n", week)
	fmt.Fprintf(&b, "This report covers the week from %s to %s. mmp %s wrote it on %s from the map, the run records of records/runs, the ledger agent/ledger.jsonl, the lab runs and the claim records (spec 10.8). The status of a node is its status on the date of the report.\n\n", day(from), day(to), Version, today)

	// The status changes.
	b.WriteString("## Status changes\n\n")
	counts := p.Counts()
	var parts []string
	for _, s := range blueprint.Statuses {
		if counts[s] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", counts[s], s))
		}
	}
	fmt.Fprintf(&b, "The map has %s: %s.\n\n", plural(len(p.Nodes), "node"), orNone(strings.Join(parts, ", ")))
	newRefuted := map[string]bool{}
	if prev == nil {
		b.WriteString("There is no status snapshot of an earlier week in records/status, so this report cannot show the changes. mmp wrote a snapshot for the next report.\n\n")
	} else {
		type change struct {
			n        *blueprint.Node
			from, to string
		}
		var changes []change
		for _, n := range p.Nodes {
			old, ok := prev.Nodes[n.Tag]
			switch {
			case !ok:
				changes = append(changes, change{n, "new", n.Status})
			case old.Status != n.Status:
				changes = append(changes, change{n, old.Status, n.Status})
			default:
				continue
			}
			if n.Status == blueprint.Refuted {
				newRefuted[n.Tag] = true
			}
		}
		var gone []string
		for tag := range prev.Nodes {
			if p.Node(tag) == nil {
				gone = append(gone, tag)
			}
		}
		sort.Strings(gone)
		changed := upperFirst(plural(len(changes), "node"))
		if len(changes) == 0 {
			changed = "No node"
		}
		fmt.Fprintf(&b, "The report compares with the snapshot of %s, from %s. %s changed status.", prev.Week, prev.Date, changed)
		if len(gone) > 0 {
			fmt.Fprintf(&b, " The map lost %s: %s.", plural(len(gone), "node"), strings.Join(gone, ", "))
		}
		b.WriteString("\n\n")
		if len(changes) > 0 {
			byTier := map[string][]change{}
			byRoute := map[string]int{}
			for _, c := range changes {
				byTier[c.n.Tier] = append(byTier[c.n.Tier], c)
				if len(c.n.Route) == 0 {
					byRoute["library"]++
				}
				for _, r := range c.n.Route {
					byRoute[r]++
				}
			}
			var tiers []string
			for t := range byTier {
				tiers = append(tiers, t)
			}
			sort.Strings(tiers)
			for _, t := range tiers {
				fmt.Fprintf(&b, "### Tier %s\n\n", orNone(t))
				for _, c := range byTier[t] {
					route := "library"
					if len(c.n.Route) > 0 {
						route = strings.Join(c.n.Route, ", ")
					}
					fmt.Fprintf(&b, "- %s, %s (%s): from %s to %s.\n", c.n.Tag, plain(c.n.Title), route, c.from, c.to)
				}
				b.WriteString("\n")
			}
			var routes []string
			for r := range byRoute {
				routes = append(routes, r)
			}
			sort.Strings(routes)
			var rp []string
			for _, r := range routes {
				rp = append(rp, fmt.Sprintf("%s %d", r, byRoute[r]))
			}
			fmt.Fprintf(&b, "By route, the changes are: %s.\n\n", strings.Join(rp, ", "))
		}
	}

	// The runs and the pull requests.
	b.WriteString("## Pull requests\n\n")
	var weekRuns []*runRecord
	byStatus := map[string]int{}
	var opened, merged []*runRecord
	for _, r := range runs {
		if inWeek(r.Date, from, to) {
			weekRuns = append(weekRuns, r)
			byStatus[r.Status]++
			if r.PullRequest != "" {
				opened = append(opened, r)
			}
		}
		if r.PullRequest != "" && inWeek(r.Merged, from, to) {
			merged = append(merged, r)
		}
	}
	if len(weekRuns) == 0 {
		b.WriteString("The agent has no run record of this week.\n\n")
	} else {
		var sp []string
		for _, s := range []string{runPlanned, runRan, runFailed, runPassed} {
			if byStatus[s] > 0 {
				sp = append(sp, fmt.Sprintf("%d %s", byStatus[s], s))
			}
		}
		fmt.Fprintf(&b, "The agent has %s of this week: %s.\n\n", plural(len(weekRuns), "run record"), strings.Join(sp, ", "))
	}
	if len(opened) == 0 && len(merged) == 0 {
		b.WriteString("No run record names a pull request that was opened or merged this week.\n\n")
	} else {
		fmt.Fprintf(&b, "The runs of this week opened %s, and %s merged this week.\n\n", plural(len(opened), "pull request"), plural(len(merged), "pull request"))
		for _, r := range opened {
			fmt.Fprintf(&b, "- Opened: %s, the role %s on %s, run %s.\n", r.PullRequest, r.Role, r.Target, r.ID)
		}
		for _, r := range merged {
			fmt.Fprintf(&b, "- Merged on %s: %s, the role %s on %s, run %s.\n", r.Merged, r.PullRequest, r.Role, r.Target, r.ID)
		}
		b.WriteString("\n")
	}

	// The spend.
	b.WriteString("## Spend\n\n")
	routeOf := map[string]string{}
	for _, r := range runs {
		routeOf[r.ID] = r.Route
	}
	byRole, byRouteSpend := map[string]float64{}, map[string]float64{}
	total, calls := 0.0, 0
	for _, e := range ledger {
		if !inWeek(e.Date, from, to) {
			continue
		}
		calls++
		total += e.Cost
		byRole[e.Role] += e.Cost
		r := routeOf[e.Run]
		if r == "" {
			r = "unknown"
		}
		byRouteSpend[r] += e.Cost
	}
	if calls == 0 {
		b.WriteString("The ledger agent/ledger.jsonl has no model call of this week, so the spend is 0.00 dollars.\n\n")
	} else {
		fmt.Fprintf(&b, "The ledger has %s of this week, for %.2f dollars.\n\n", plural(calls, "model call"), total)
		for _, kv := range []struct {
			name string
			m    map[string]float64
		}{{"By role", byRole}, {"By route", byRouteSpend}} {
			var keys []string
			for k := range kv.m {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			var sp []string
			for _, k := range keys {
				sp = append(sp, fmt.Sprintf("%s %.2f", k, kv.m[k]))
			}
			fmt.Fprintf(&b, "%s, in dollars: %s.\n\n", kv.name, strings.Join(sp, ", "))
		}
	}

	// The lab runs.
	b.WriteString("## Lab runs\n\n")
	var weekLab []*lab.Record
	for _, r := range labRuns {
		if inWeek(r.Started, from, to) {
			weekLab = append(weekLab, r)
		}
	}
	if len(weekLab) == 0 {
		b.WriteString("The lab has no run of this week in lab/runs.\n\n")
	} else {
		fmt.Fprintf(&b, "The lab has %s of this week.\n\n", plural(len(weekLab), "run"))
		for _, r := range weekLab {
			fmt.Fprintf(&b, "- %s: the experiment %s on %s, result %s, %s.\n", r.ID, r.Experiment, orNone(r.Node), orNone(r.Result), mismatches(r.Mismatches))
		}
		b.WriteString("\n")
	}

	// The claim checks.
	b.WriteString("## Claim checks\n\n")
	if len(claims) == 0 {
		b.WriteString("There is no claim record in claims/.\n\n")
	} else {
		added, letters, answers := 0, 0, 0
		for _, e := range claims {
			c := e.Record
			if inWeek(c.Date, from, to) {
				added++
			}
			for _, l := range c.Letters {
				if inWeek(l.Date, from, to) {
					letters++
				}
				if inWeek(l.AnswerDate, from, to) {
					answers++
				}
			}
		}
		fmt.Fprintf(&b, "The directory claims/ has %s. This week %s came in, %s went to authors and %s came back.\n\n", plural(len(claims), "claim record"), plural(added, "claim"), plural(letters, "letter"), plural(answers, "answer"))
		for _, e := range claims {
			c := e.Record
			n := map[string]int{}
			var gaps []string
			for _, pt := range c.Parts {
				n[pt.Status]++
				if pt.Status == "gap" {
					gaps = append(gaps, pt.ID)
				}
			}
			line := fmt.Sprintf("- %s, status %s: %s, %d done", c.Key, c.Status, plural(len(c.Parts), "part"), n["done"])
			if len(gaps) > 0 {
				line += fmt.Sprintf(", %s found (%s)", plural(len(gaps), "gap"), strings.Join(gaps, ", "))
			} else {
				line += ", no gap found"
			}
			var sent []string
			for _, l := range c.Letters {
				if inWeek(l.Date, from, to) {
					sent = append(sent, plain(l.To))
				}
			}
			if len(sent) > 0 {
				line += "; a letter went this week to " + strings.Join(sent, ", ")
			}
			b.WriteString(line + ".\n")
		}
		b.WriteString("\n")
	}

	// The refuted conjectures.
	b.WriteString("## Refuted conjectures\n\n")
	var refuted []*blueprint.Node
	for _, n := range p.Nodes {
		if n.Status == blueprint.Refuted {
			refuted = append(refuted, n)
		}
	}
	if len(refuted) == 0 {
		b.WriteString("No node of the map is refuted.\n\n")
	} else {
		fmt.Fprintf(&b, "The map has %s.\n\n", plural(len(refuted), "refuted node"))
		for _, n := range refuted {
			when := ""
			if newRefuted[n.Tag] {
				when = " It is new since the last snapshot."
			}
			fmt.Fprintf(&b, "- %s, %s: the negation %s builds.%s\n", n.Tag, plain(n.Title), orNone(n.Lean.Negation), when)
		}
		b.WriteString("\n")
	}

	// The largest open nodes.
	b.WriteString("## The largest open nodes\n\n")
	b.WriteString("A node is open here when it is not proved, not proved-mod, not defined and not refuted. The size of a node is the number of the nodes that use it, directly or through other nodes. The list gives the five largest open nodes of each route and of the library.\n\n")
	// The routes of routes.yaml, and the routes that a node names, so a node is not lost when routes.yaml is missing.
	var groups []string
	for _, r := range p.Routes {
		if !contains(groups, r.ID) {
			groups = append(groups, r.ID)
		}
	}
	var extra []string
	for _, n := range p.Nodes {
		for _, r := range n.Route {
			if !contains(groups, r) && !contains(extra, r) {
				extra = append(extra, r)
			}
		}
	}
	sort.Strings(extra)
	groups = append(append(groups, extra...), "library")
	for _, g := range groups {
		type sized struct {
			n    *blueprint.Node
			size int
		}
		var list []sized
		for _, n := range p.Nodes {
			if done(n) || n.Status == blueprint.Refuted {
				continue
			}
			if g == "library" && len(n.Route) == 0 || contains(n.Route, g) {
				list = append(list, sized{n, users(p, n)})
			}
		}
		sort.SliceStable(list, func(i, j int) bool {
			if list[i].size != list[j].size {
				return list[i].size > list[j].size
			}
			return list[i].n.Tag < list[j].n.Tag
		})
		name := "The route " + g
		if g == "library" {
			name = "The library"
		}
		if len(list) == 0 {
			fmt.Fprintf(&b, "### %s\n\n%s has no open node.\n\n", name, name)
			continue
		}
		fmt.Fprintf(&b, "### %s\n\n", name)
		if len(list) > 5 {
			list = list[:5]
		}
		for _, s := range list {
			fmt.Fprintf(&b, "- %s, %s: %s, used by %s; %s.\n", s.n.Tag, plain(s.n.Title), s.n.Status, plural(s.size, "node"), blockers(p, s.n))
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}
