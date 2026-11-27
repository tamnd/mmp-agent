package lab

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// bruteKeys4 lists the subgroups of order r of (Z/r)^4 with no nonzero element on one coordinate, and returns their normal forms by elements. It grows the subgroups one element at a time from the trivial group, and keeps only the groups whose order divides r and with no element on one coordinate: a subgroup of a group with this property has it too.
func bruteKeys4(r int) map[string]bool {
	n := r * r * r * r
	vec := func(i int) vec4 { return vec4{i / (r * r * r), i / (r * r) % r, i / r % r, i % r} }
	var ok []vec4
	for i := 1; i < n; i++ {
		if v := vec(i); nonzero4(v) != 1 {
			ok = append(ok, v)
		}
	}
	type group struct {
		gens  []vec4
		elems map[vec4]bool
	}
	name := func(elems []vec4) string {
		sort.Slice(elems, func(a, b int) bool { return less(elems[a][:], elems[b][:]) })
		return fmt.Sprint(elems)
	}
	seen := map[string]bool{}
	queue := []group{{nil, map[vec4]bool{{}: true}}}
	keys := map[string]bool{}
	for len(queue) > 0 {
		g := queue[0]
		queue = queue[1:]
		if len(g.elems) == r {
			var gs [][]int
			for _, v := range g.gens {
				gs = append(gs, []int{v[0], v[1], v[2], v[3]})
			}
			keys[CanonKey4(r, gs)] = true
			continue
		}
		for _, x := range ok {
			if g.elems[x] {
				continue
			}
			gens := append(append([]vec4(nil), g.gens...), x)
			elems := closure4(r, gens)
			if r%len(elems) != 0 {
				continue
			}
			good := true
			for _, e := range elems {
				good = good && nonzero4(e) != 1
			}
			if k := name(elems); good && !seen[k] {
				seen[k] = true
				set := map[vec4]bool{}
				for _, e := range elems {
					set[e] = true
				}
				queue = append(queue, group{gens, set})
			}
		}
	}
	return keys
}

func TestSimplicial4AgainstBruteForce(t *testing.T) {
	maxR := 8
	if testing.Short() {
		maxR = 4
	}
	for r := 2; r <= maxR; r++ {
		want := bruteKeys4(r)
		got := map[string]bool{}
		for _, g := range SimplicialGerms4(r) {
			k := CanonKey4(r, g.Gens)
			if got[k] {
				t.Errorf("r = %d: the group %v is listed twice", r, g.Gens)
			}
			got[k] = true
			var gs []vec4
			for _, v := range g.Gens {
				gs = append(gs, vec4{v[0], v[1], v[2], v[3]})
			}
			c, err := GermOfGens4(r, gs)
			if err != nil || describe(c) != describe(g) || c.Cyclic != g.Cyclic || c.Isolated != g.Isolated || c.GorIndex != g.GorIndex || fmt.Sprint(c.Weights) != fmt.Sprint(g.Weights) {
				t.Errorf("r = %d, %v: the enumeration gives %s, the closure %s (%v)", r, g.Gens, describe(g), describe(c), err)
			}
		}
		if len(got) != len(want) {
			t.Errorf("r = %d: %d germs, but the brute force finds %d", r, len(got), len(want))
		}
		for k := range want {
			if !got[k] {
				t.Errorf("r = %d: the enumeration misses %s", r, k)
			}
		}
	}
}

func TestSimplicial4SmallIndices(t *testing.T) {
	// r = 2: the nonzero vector of G has 2, 3 or 4 coordinates 1/2, so 1/2(1,1,0,0), 1/2(1,1,1,0) and 1/2(1,1,1,1).
	gs := SimplicialGerms4(2)
	var ws []string
	for _, g := range gs {
		ws = append(ws, fmt.Sprint(g.Weights))
	}
	sort.Strings(ws)
	if fmt.Sprint(ws) != "[[0 0 1 1] [0 1 1 1] [1 1 1 1]]" {
		t.Errorf("r = 2: the germs are %v", ws)
	}
	// r = 8 has the group (Z/2)^3, of rank 3.
	found := false
	for _, g := range SimplicialGerms4(8) {
		found = found || fmt.Sprint(g.Group) == "[2 2 2]"
	}
	if !found {
		t.Error("r = 8 has no germ with the group (Z/2)^3")
	}
}

func TestGroupOf(t *testing.T) {
	for _, tc := range []struct {
		r    int
		gens []vec4
		want string
	}{
		{6, []vec4{{1, 5, 2, 4}}, "[6]"},
		{4, []vec4{{2, 2, 0, 0}, {0, 0, 2, 2}}, "[2 2]"},
		{8, []vec4{{4, 4, 0, 0}, {4, 0, 4, 0}, {4, 0, 0, 4}}, "[2 2 2]"},
		{12, []vec4{{6, 6, 0, 0}, {0, 2, 2, 2}}, "[2 6]"},
	} {
		if got := fmt.Sprint(groupOf(tc.r, closure4(tc.r, tc.gens))); got != tc.want {
			t.Errorf("r = %d, %v: the group is %s, want %s", tc.r, tc.gens, got, tc.want)
		}
	}
}

func TestHandCases4(t *testing.T) {
	r, err := RunE2Simplicial4(E2Inputs{Dimension: 4, MaxIndex: 8, Family: "simplicial"})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Hand) != len(handCases4) {
		t.Fatalf("the run has %d of the %d hand cases", len(r.Hand), len(handCases4))
	}
	for _, h := range r.Hand {
		if !h.Found || h.Got != h.Want {
			t.Errorf("%s: found %v, got %q, want %q", h.Name, h.Found, h.Got, h.Want)
		}
	}
	g, _ := GermOfGens4(5, []vec4{{1, 1, 1, 1}})
	if g.GorIndex != 5 || !g.EncHL || !g.Isolated || g.Terminal {
		t.Errorf("1/5(1,1,1,1) is %+v", g)
	}
	g, _ = GermOfGens4(5, []vec4{{1, 2, 3, 4}})
	if g.GorIndex != 1 || !pairType(g) {
		t.Errorf("1/5(1,2,3,4) is %+v", g)
	}
	if _, err := GermOfGens4(2, [][4]int{{1, 0, 0, 0}}); err == nil {
		t.Error("1/2(1,0,0,0) is accepted, but its first ray is not primitive")
	}
}

func TestSimplicial4Checks(t *testing.T) {
	r, err := RunE2Simplicial4(E2Inputs{Dimension: 4, MaxIndex: 16, Family: "simplicial", ListIndex: 5})
	if err != nil {
		t.Fatal(err)
	}
	if r.Mismatches() != 0 || r.Gaps() != 0 || !strings.HasPrefix(e2s4Result(r), "agrees") {
		t.Fatalf("the run has mismatches: %v %v %v", r.CyclicFails, r.LineFails, r.NoTerminal)
	}
	old, _ := RunE2(E2Inputs{Dimension: 4, MaxIndex: 16})
	three, _ := RunE2Simplicial(E2Inputs{Dimension: 3, MaxIndex: 16, Family: "simplicial"})
	if r.CyclicGerms != len(old.Germs) || r.LineGerms != three.Total.Germs {
		t.Errorf("the comparisons have %d and %d germs, want %d and %d", r.CyclicGerms, r.LineGerms, len(old.Germs), three.Total.Germs)
	}
	for i, x := range r.Indices {
		if x.Line != three.Indices[i].Germs {
			t.Errorf("index %d: %d germs with a zero coordinate, but %d 3-fold germs", x.Index, x.Line, three.Indices[i].Germs)
		}
	}
	for _, g := range r.Germs {
		if g.Index > 5 {
			t.Fatalf("germs.json lists r = %d above list_index 5", g.Index)
		}
	}
	// A wrong prediction is caught.
	r.LineFails = []string{"x"}
	if r.Mismatches() != 1 || !strings.HasPrefix(e2s4Result(r), "mismatch") {
		t.Errorf("a failed check gives %q", e2s4Result(r))
	}
	r = S4Result{NoTerminal: []int{5}}
	if r.Gaps() != 1 || !strings.HasPrefix(e2s4Result(r), "incomplete") {
		t.Errorf("a gap gives %q", e2s4Result(r))
	}
}

func TestHNF4(t *testing.T) {
	got := hnf4([4][4]int{{2, 1, 0, 0}, {0, 3, 0, 0}, {1, 1, 1, 0}, {0, 0, 0, 1}})
	if got != [4][4]int{{1, 0, 5, 0}, {0, 1, 2, 0}, {0, 0, 6, 0}, {0, 0, 0, 1}} {
		t.Errorf("the normal form is %v", got)
	}
	h := dual4{{1, 1, 2, 3}, {0, 2, 1, 0}, {0, 0, 3, 1}, {0, 0, 0, 2}}
	for _, c := range h.adjColumns(12) {
		if !h.has(12, c) {
			t.Errorf("the column %v of the adjugate is not in N", c)
		}
	}
}

const simplicialSpec4 = `name: e2-simplicial-small-4
experiment: E2
question: Do the small simplicial cases of dimension 4 agree?
tool: mmp
node: MM00000001
inputs: {dimension: 4, max_index: 8, family: simplicial}
outputs: [germs.json, indices.json, summary.md]
notes: The notes of the run.
`

func TestRunSimplicial4(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "lab", "specs", "e2-simplicial-small-4.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(simplicialSpec4), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := LoadSpec(path)
	if err != nil {
		t.Fatal(err)
	}
	opt := Options{Runs: RunsDir(path), Host: "test", ToolVersion: "dev"}
	rec, err := Run(s, opt)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Mismatches != 0 || rec.Gaps != 0 || !strings.HasPrefix(rec.Result, "agrees") || len(rec.Files) != 3 {
		t.Fatalf("the record is %+v", rec)
	}
	dir := filepath.Join(opt.Runs, rec.ID)
	summary, _ := os.ReadFile(filepath.Join(dir, "summary.md"))
	for _, want := range []string{"## The checks", "| 1/5(1,1,1,1) |", "| (Z/2)^3 |", "## Notes\n\nThe notes of the run.", "| all |", "gorenstein"} {
		if !strings.Contains(string(summary), want) {
			t.Errorf("the summary has no %q:\n%s", want, summary)
		}
	}
	germs, _ := os.ReadFile(filepath.Join(dir, "germs.json"))
	if !strings.Contains(string(germs), `"gorenstein_index":`) {
		t.Errorf("germs.json has no Gorenstein index: %.200q", germs)
	}
}
