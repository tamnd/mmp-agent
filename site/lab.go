package site

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// LabURL is the address of the page of a run of the lab.
func LabURL(id string) string { return "/lab/" + id + "/" }

// LabRecord is record.json of a run of the lab (spec 13). A run answers one question with a computation. The record names the spec, the tool, the node that the run supports or refutes, and the files of the output.
type LabRecord struct {
	ID          string            `json:"id"`
	Experiment  string            `json:"experiment"`
	Question    string            `json:"question"`
	Spec        string            `json:"spec"`
	SpecSHA256  string            `json:"spec_sha256"`
	Node        string            `json:"node"`
	Nodes       []string          `json:"nodes,omitempty"`
	Tool        string            `json:"tool"`
	ToolVersion string            `json:"tool_version"`
	Commit      string            `json:"commit,omitempty"`
	Host        string            `json:"host"`
	Started     string            `json:"started"`
	WallSeconds float64           `json:"wall_seconds"`
	Result      string            `json:"result"`
	Files       map[string]string `json:"files"`
}

// LabRun is the data of a run of the lab, for the list of runs and for the page of the run.
type LabRun struct {
	Rec     *LabRecord
	URL     string
	Node    NodeLink
	Nodes   []NodeLink
	Files   []LabFile
	Wall    string
	SpecURL string
	ToolURL string

	dir string // the directory of the run in the content repository
	md  string // summary.md
}

// LabFile is a file of a run that the site copies.
type LabFile struct {
	Name, URL string
	Size      string
}

// labDefault is the text of the lab page when the content has no site/lab.md.
const labDefault = "Each run of the lab answers one question with a computation. The spec is in `lab/specs/` and the outputs are in `lab/runs/` of the content repository. A run is data from examples. It does not prove a statement.\n"

// loadLabRecord reads record.json of one run directory.
func loadLabRecord(dir string) (*LabRecord, error) {
	data, err := os.ReadFile(filepath.Join(dir, "record.json")) // #nosec G304 -- a file of the content repository
	if err != nil {
		return nil, err
	}
	var rec LabRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, fmt.Errorf("%s: %w", dir, err)
	}
	if rec.ID == "" || rec.ID != filepath.Base(dir) {
		return nil, fmt.Errorf("%s: the id %q of record.json is not the name of the directory", dir, rec.ID)
	}
	return &rec, nil
}

// labRuns reads lab/runs of the content repository. A missing directory gives no runs.
func (b *builder) labRuns() ([]LabRun, error) {
	root := filepath.Join(b.opt.Content, "lab", "runs")
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("site: %w", err)
	}
	var out []LabRun
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		rec, err := loadLabRecord(dir)
		if err != nil {
			return nil, fmt.Errorf("site: lab run: %w", err)
		}
		md, _ := os.ReadFile(filepath.Join(dir, "summary.md")) // #nosec G304 -- a file of the content repository
		lr := LabRun{
			Rec: rec, URL: LabURL(rec.ID), Node: b.nodeLink(rec.Node), Nodes: b.nodeLinks(rec.Nodes),
			Wall: wallText(rec.WallSeconds), SpecURL: ContentRepo + "/blob/main/" + rec.Spec, dir: dir, md: string(md),
		}
		if rec.Commit != "" {
			lr.ToolURL = AgentRepo + "/commit/" + rec.Commit
		}
		names := make([]string, 0, len(rec.Files)+1)
		for n := range rec.Files {
			names = append(names, n)
		}
		names = append(names, "record.json")
		sort.Strings(names)
		for _, n := range names {
			info, err := os.Stat(filepath.Join(dir, n))
			if err != nil {
				return nil, fmt.Errorf("site: lab run %s: %w", rec.ID, err)
			}
			lr.Files = append(lr.Files, LabFile{Name: n, URL: lr.URL + n, Size: sizeText(info.Size())})
		}
		out = append(out, lr)
	}
	return out, nil
}

func wallText(s float64) string {
	switch {
	case s < 60:
		return fmt.Sprintf("%.1f s", s)
	case s < 3600:
		return fmt.Sprintf("%.1f min", s/60)
	}
	return fmt.Sprintf("%.2f h", s/3600)
}

func sizeText(n int64) string {
	switch {
	case n < 1<<10:
		return fmt.Sprintf("%d B", n)
	case n < 1<<20:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
}

// labViews adds the page of each run. The list of runs is the page lab/, from site/lab.md or from labDefault.
func (b *builder) labViews(runs []LabRun) {
	for i := range runs {
		r := runs[i]
		b.add(&view{
			Path: strings.TrimPrefix(r.URL, "/"), Title: r.Rec.ID, Section: "lab", tmpl: "labrun", Lab: &r,
			Description: r.Rec.Experiment + ". " + r.Rec.Question,
			SourceURL:   ContentRepo + "/tree/main/lab/runs/" + r.Rec.ID,
			Trail:       append(home(), Crumb{Text: "Lab", URL: "/lab/"}, Crumb{Text: r.Rec.ID, Current: true}),
			markdown:    r.summary(),
		})
	}
}

// summary returns summary.md of the run without its title, which the page shows.
func (r *LabRun) summary() string {
	s := r.md
	if strings.HasPrefix(s, "# ") {
		if i := strings.Index(s, "\n"); i >= 0 {
			s = s[i+1:]
		}
	}
	return strings.TrimLeft(s, "\n")
}

// labCopy writes the files of each run next to its page.
func (b *builder) labCopy(w *writer, runs []LabRun) {
	for _, r := range runs {
		for _, f := range r.Files {
			data, err := os.ReadFile(filepath.Join(r.dir, f.Name)) // #nosec G304 -- a file of the content repository
			if err != nil {
				w.err = err
				return
			}
			w.file(strings.TrimPrefix(f.URL, "/"), data)
		}
	}
}
