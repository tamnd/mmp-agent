package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/tamnd/mmp-agent/blueprint"
)

// costPart is a part of the spend of the window: the calls and the dollars.
type costPart struct {
	calls int
	cost  float64
}

func (c *costPart) add(e ledgerEntry) {
	c.calls++
	c.cost += e.Cost
}

// costNode is the spend of the window on one node.
type costNode struct {
	n *blueprint.Node
	costPart
}

func agentCost(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("mmp agent cost", flag.ContinueOnError)
	flags.SetOutput(stderr)
	content := flags.String("content", ".", "the root of the content repository")
	fromFlag := flags.String("from", "", "the first day of the window, YYYY-MM-DD (default: the first entry of the ledger)")
	toFlag := flags.String("to", "", "the last day of the window, YYYY-MM-DD (default: today, UTC)")
	write := flags.Bool("write", false, "also write the report to reports/cost-<to>.md")
	if err := parse(flags, args); err != nil {
		return err
	}
	var from time.Time
	if *fromFlag != "" {
		d, err := time.Parse("2006-01-02", *fromFlag)
		if err != nil {
			return fmt.Errorf("%w: the date %q of -from is not YYYY-MM-DD", errUsage, *fromFlag)
		}
		from = d
	}
	to := time.Now().UTC().Truncate(24 * time.Hour)
	if *toFlag != "" {
		d, err := time.Parse("2006-01-02", *toFlag)
		if err != nil {
			return fmt.Errorf("%w: the date %q of -to is not YYYY-MM-DD", errUsage, *toFlag)
		}
		to = d
	}
	if to.Before(from) {
		return fmt.Errorf("%w: the window ends on %s, before it starts on %s", errUsage, to.Format("2006-01-02"), from.Format("2006-01-02"))
	}
	if _, err := os.Stat(filepath.Join(*content, "blueprint")); err != nil {
		return fmt.Errorf("%s is not the content repository: %w", *content, err)
	}
	p, err := blueprint.Load(*content)
	if err != nil {
		return err
	}
	runs, err := loadRuns(*content)
	if err != nil {
		return err
	}
	hasLedger := true
	if _, err := os.Stat(filepath.Join(*content, "agent", "ledger.jsonl")); errors.Is(err, fs.ErrNotExist) {
		hasLedger = false
	}
	ledger, err := loadLedger(*content, runs)
	if err != nil {
		return err
	}
	audit, err := loadAudit(*content)
	if errors.Is(err, fs.ErrNotExist) {
		audit, err = nil, nil
	}
	if err != nil {
		return err
	}
	text := costText(p, runs, ledger, hasLedger, audit, from, to)
	fmt.Fprint(stdout, text)
	if !*write {
		return nil
	}
	dst := filepath.Join(*content, "reports", "cost-"+to.Format("2006-01-02")+".md")
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return err
	}
	if err := os.WriteFile(dst, []byte(text), 0o644); err != nil { // #nosec G306 -- a public report
		return err
	}
	fmt.Fprintf(stderr, "wrote %s\n", dst)
	return nil
}

// costText writes the cost report of milestone S2: the spend of the window, the cost per proved node and the cost per audited definition.
func costText(p *blueprint.Project, runs []*runRecord, ledger []ledgerEntry, hasLedger bool, audit *blueprint.Audit, from, to time.Time) string {
	var b strings.Builder
	day := func(t time.Time) string { return t.Format("2006-01-02") }
	toDay := day(to)
	fromText := "the first entry of the ledger"
	if !from.IsZero() {
		fromText = day(from)
	}
	fmt.Fprintf(&b, "# The cost report to %s\n\n", toDay)
	fmt.Fprintf(&b, "This report covers the model calls of the ledger agent/ledger.jsonl from %s to %s. mmp %s wrote it from the ledger, the run records of records/runs, the map and records/audit.json (spec 10.6, milestone S2). The status of a node is its status now, not on the date of the call.\n\n", fromText, toDay, Version)

	byID := map[string]*runRecord{}
	for _, r := range runs {
		byID[r.ID] = r
	}
	var proved, open, claims, lost, unattributed costPart
	nodes := map[string]*costNode{}
	auditor := costPart{}
	var undated costPart
	for _, e := range ledger {
		if e.Date == "" {
			undated.add(e)
			continue
		}
		if !inWeek(e.Date, from, to) {
			continue
		}
		if e.Role == "auditor" {
			auditor.add(e)
		}
		r := byID[e.Run]
		switch {
		case r == nil:
			unattributed.add(e)
		case r.Kind == "claim":
			claims.add(e)
		case p.Node(r.Target) == nil:
			lost.add(e)
		default:
			n := p.Node(r.Target)
			cn := nodes[n.Tag]
			if cn == nil {
				cn = &costNode{n: n}
				nodes[n.Tag] = cn
			}
			cn.add(e)
			if n.Status == blueprint.Proved {
				proved.add(e)
			} else {
				open.add(e)
			}
		}
	}
	sp := sumSpend(ledger, runs, from, to)

	// The spend.
	b.WriteString("## Spend\n\n")
	switch {
	case !hasLedger:
		b.WriteString("There is no ledger agent/ledger.jsonl, so no spend is recorded. The spend is 0.00 dollars.\n\n")
	case sp.calls == 0:
		b.WriteString("The ledger has no model call in the window, so the spend is 0.00 dollars.\n\n")
	default:
		fmt.Fprintf(&b, "The ledger has %s in the window, for %.2f dollars.\n\n", plural(sp.calls, "model call"), sp.total)
		for _, kv := range []struct {
			name string
			m    map[string]float64
		}{{"Role", sp.byRole}, {"Route", sp.byRoute}} {
			fmt.Fprintf(&b, "| %s | Dollars |\n|---|---:|\n", kv.name)
			for _, k := range sortedKeys(kv.m) {
				fmt.Fprintf(&b, "| %s | %.2f |\n", orNone(k), kv.m[k])
			}
			b.WriteString("\n")
		}
		b.WriteString("The route unknown is the spend of the calls whose run has no record.\n\n")
		b.WriteString("Each call of the window is in one row of this table, so the rows add up to the total.\n\n")
		b.WriteString("| Target of the run | Calls | Dollars |\n|---|---:|---:|\n")
		for _, row := range []struct {
			name string
			c    costPart
		}{{"a node that is proved now", proved}, {"a node that is not proved yet", open}, {"a claim", claims}, {"a node that is not in the map now", lost}, {"unattributed: the run has no record", unattributed}} {
			fmt.Fprintf(&b, "| %s | %d | %.2f |\n", row.name, row.c.calls, row.c.cost)
		}
		fmt.Fprintf(&b, "| total | %d | %.2f |\n\n", sp.calls, sp.total)
	}
	if undated.calls > 0 {
		fmt.Fprintf(&b, "The ledger also has %s with no date, for %.2f dollars: neither the entry nor its run has a date. They are in no window, so the tables leave them out.\n\n", plural(undated.calls, "model call"), undated.cost)
	}

	// The cost per proved node.
	b.WriteString("## Cost per proved node\n\n")
	var provedNodes, openNodes []*costNode
	for _, tag := range sortedKeys(nodes) {
		if cn := nodes[tag]; cn.n.Status == blueprint.Proved {
			provedNodes = append(provedNodes, cn)
		} else {
			openNodes = append(openNodes, cn)
		}
	}
	all := p.Counts()[blueprint.Proved]
	fmt.Fprintf(&b, "A node counts here when its status is proved now. The map has %s.", plural(all, "proved node"))
	if len(provedNodes) == 0 {
		b.WriteString(" No run of the window was on a proved node, so the cost per proved node is not defined yet.\n\n")
	} else {
		fmt.Fprintf(&b, " The runs of the window on %d of them spent %.2f dollars, so the cost per proved node is %.2f dollars.", len(provedNodes), proved.cost, proved.cost/float64(len(provedNodes)))
		if left := all - len(provedNodes); left > 0 {
			fmt.Fprintf(&b, " The other %d have no spend in the window.", left)
		}
		b.WriteString("\n\n")
		writeCostNodes(&b, provedNodes, false)
	}
	b.WriteString("### Work in progress\n\n")
	if len(openNodes) == 0 {
		b.WriteString("No run of the window was on a node that is not proved yet.\n\n")
	} else {
		fmt.Fprintf(&b, "The runs of the window on nodes that are not proved yet spent %.2f dollars, on %s. This spend is not in the cost per proved node.\n\n", open.cost, plural(len(openNodes), "node"))
		writeCostNodes(&b, openNodes, true)
	}

	// The cost per audited definition.
	b.WriteString("## Cost per audited definition\n\n")
	fmt.Fprintf(&b, "The role auditor spent %.2f dollars in the window, in %s.", auditor.cost, plural(auditor.calls, "model call"))
	if audit == nil {
		b.WriteString(" There is no records/audit.json, so no definition is audited and the cost per audited definition is not defined yet.\n")
		return strings.TrimRight(b.String(), "\n") + "\n"
	}
	a5 := audit.Steps["A5"]
	if a5 == "" {
		a5 = "not started"
	}
	fmt.Fprintf(&b, " records/audit.json keeps the state of the steps A1 to A8 and the number of open findings, and it counts no audited definition. Step A5, the reading of each definition of the index of A4, is %s there. So the number of audited definitions in the window is 0, and the cost per audited definition is not defined yet. It needs one record per definition of A5 (spec 04.3).\n", a5)
	return strings.TrimRight(b.String(), "\n") + "\n"
}

// writeCostNodes writes a table of nodes with their spend, the largest spend first.
func writeCostNodes(b *strings.Builder, list []*costNode, status bool) {
	sort.SliceStable(list, func(i, j int) bool { return list[i].cost > list[j].cost })
	if status {
		b.WriteString("| Node | Title | Status | Calls | Dollars |\n|---|---|---|---:|---:|\n")
	} else {
		b.WriteString("| Node | Title | Calls | Dollars |\n|---|---|---:|---:|\n")
	}
	for _, cn := range list {
		title := strings.ReplaceAll(plain(cn.n.Title), "|", "/")
		if status {
			fmt.Fprintf(b, "| %s | %s | %s | %d | %.2f |\n", cn.n.Tag, title, cn.n.Status, cn.calls, cn.cost)
		} else {
			fmt.Fprintf(b, "| %s | %s | %d | %.2f |\n", cn.n.Tag, title, cn.calls, cn.cost)
		}
	}
	b.WriteString("\n")
}
