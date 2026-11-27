package lab

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// bruteKeys lists the subgroups of order r of (Z/r)^3 with no nonzero element on one coordinate, by the closure of each pair of elements, and returns their normal forms by elements. A group with no element on one coordinate has rank at most 2, because a group of rank 3 contains all of (Z/p)^3 for some prime p, so pairs suffice.
func bruteKeys(r int) map[string]bool {
	n := r * r * r
	vec := func(i int) [3]int { return [3]int{i / (r * r), i / r % r, i % r} }
	keys, groups := map[string]bool{}, map[string]bool{}
	for x := range n {
		if nonzero(vec(x)) == 1 {
			continue
		}
		for y := x; y < n; y++ {
			if nonzero(vec(y)) == 1 {
				continue
			}
			elems := closure(r, [][3]int{vec(x), vec(y)})
			if len(elems) != r {
				continue
			}
			ok := true
			for _, e := range elems {
				ok = ok && nonzero(e) != 1
			}
			if !ok {
				continue
			}
			sort.Slice(elems, func(i, j int) bool { return less(elems[i][:], elems[j][:]) })
			if k := fmt.Sprint(elems); !groups[k] {
				groups[k] = true
				a, b := vec(x), vec(y)
				keys[CanonKey(r, [][]int{a[:], b[:]})] = true
			}
		}
	}
	return keys
}

func TestSimplicialAgainstBruteForce(t *testing.T) {
	for r := 2; r <= 8; r++ {
		want := bruteKeys(r)
		got := map[string]bool{}
		for _, g := range SimplicialGerms(r) {
			k := CanonKey(r, g.Gens)
			if got[k] {
				t.Errorf("r = %d: the group %v is listed twice", r, g.Gens)
			}
			got[k] = true
			var gs [][3]int
			for _, v := range g.Gens {
				gs = append(gs, [3]int{v[0], v[1], v[2]})
			}
			c, err := GermOfGens(r, gs)
			if err != nil || describe(c) != describe(g) || c.Cyclic != g.Cyclic || c.Isolated != g.Isolated || fmt.Sprint(c.Weights) != fmt.Sprint(g.Weights) {
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

func TestSimplicialSmallIndices(t *testing.T) {
	// r = 2: 1/2(1,1,0) and 1/2(1,1,1). r = 3: 1/3(1,1,0), 1/3(1,2,0), 1/3(1,1,1) and 1/3(1,1,2). r = 4: the cyclic groups 1/4(1,1,2), 1/4(1,3,2), 1/4(1,1,1), 1/4(1,1,3), 1/4(1,3,0), 1/4(1,1,0), and the Klein group.
	for r, want := range map[int][]string{
		2: {"[1 1 0]", "[1 1 1]"},
		3: {"[1 1 0]", "[1 2 0]", "[1 1 1]", "[1 1 2]"},
	} {
		gs := SimplicialGerms(r)
		var got []string
		for _, g := range gs {
			got = append(got, fmt.Sprint(g.Weights))
		}
		if len(gs) != len(want) {
			t.Errorf("r = %d: the germs are %v, want %v", r, got, want)
		}
	}
	gs := SimplicialGerms(4)
	cyc := 0
	for _, g := range gs {
		if g.Cyclic {
			cyc++
		}
	}
	if len(gs) != 7 || cyc != 6 {
		t.Errorf("r = 4: %d germs, %d cyclic, want 7 and 6", len(gs), cyc)
	}
}

func TestHandCases(t *testing.T) {
	r, err := RunE2Simplicial(E2Inputs{Dimension: 3, MaxIndex: 10, Family: "simplicial"})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Hand) != len(handCases) {
		t.Fatalf("the run has %d of the %d hand cases", len(r.Hand), len(handCases))
	}
	for _, h := range r.Hand {
		if !h.Found || h.Got != h.Want {
			t.Errorf("%s: found %v, got %q, want %q", h.Name, h.Found, h.Got, h.Want)
		}
	}
	// The hand cases by fields, not by strings.
	g, err := GermOfGens(2, [][3]int{{1, 1, 0}})
	if err != nil {
		t.Fatal(err)
	}
	if g.MldNum != 2 || g.MldDen != 1 || g.GermMldNum != 1 || g.GermMldDen != 1 || !g.Canon || g.Terminal || g.Isolated || !g.Cyclic || g.AtMost1 != 1 || g.AtMost1Point != 0 || !g.Enc || g.EncHL || g.Lemma != nil {
		t.Errorf("1/2(1,1,0) is %+v", g)
	}
	g, err = GermOfGens(4, [][3]int{{2, 2, 0}, {2, 0, 2}})
	if err != nil {
		t.Fatal(err)
	}
	if g.MldNum != 2 || g.GermMldNum != 1 || g.GermMldDen != 1 || !g.Canon || g.Terminal || g.Cyclic || fmt.Sprint(g.Group) != "[2 2]" || g.AtMost1 != 3 || g.Enc {
		t.Errorf("the Klein group is %+v", g)
	}
	if _, err := GermOfGens(2, [][3]int{{1, 0, 0}}); err == nil {
		t.Error("1/2(1,0,0) is accepted, but its first ray is not primitive")
	}
	if _, err := GermOfGens(4, [][3]int{{2, 2, 0}}); err == nil {
		t.Error("a group of order 2 is accepted as a group of order 4")
	}
	// 1/7(1,1,1): the box point (2,2,2)/7 has age 6/7 but it is twice (1,1,1)/7, so the germ has one divisor of log discrepancy at most 1 and is enc. The family cyclic counts one too.
	g, _ = GermOfGens(7, [][3]int{{1, 1, 1}})
	if g.AtMost1 != 1 || !g.EncHL {
		t.Errorf("1/7(1,1,1) is %+v", g)
	}
}

func TestSimplicialAgainstCyclic(t *testing.T) {
	r, err := RunE2Simplicial(E2Inputs{Dimension: 3, MaxIndex: 40, Family: "simplicial", ListIndex: 10})
	if err != nil {
		t.Fatal(err)
	}
	if r.Mismatches() != 0 || r.Gaps() != 0 {
		t.Fatalf("the run has mismatches: %v %v %v %d %v", r.TermFails, r.LemmaFails, r.CompareFails, r.IsolatedNotCy, r.NoTerminal)
	}
	old, _ := RunE2(E2Inputs{Dimension: 3, MaxIndex: 40})
	if r.CompareIndex != 40 || r.CompareGerms != len(old.Germs) || r.Total.Isolated != len(old.Germs) {
		t.Errorf("the comparison has %d germs to %d, and the run %d isolated germs; the family cyclic has %d", r.CompareGerms, r.CompareIndex, r.Total.Isolated, len(old.Germs))
	}
	for i, x := range r.Indices {
		o := old.Indices[i]
		if x.Index != o.Index || x.Terminal != o.Terminal || x.Isolated != o.Germs {
			t.Errorf("index %d: %+v, the family cyclic %+v", x.Index, x, o)
		}
	}
	for _, g := range r.Germs {
		if g.Index > 10 {
			t.Fatalf("germs.json lists r = %d above list_index 10", g.Index)
		}
	}
}

func TestSimplicialWrongPredictionIsCaught(t *testing.T) {
	g, _ := GermOfGens(5, [][3]int{{1, 1, 3}})
	r := SResult{TermFails: []SGerm{g}}
	if r.Mismatches() != 1 || !strings.HasPrefix(e2sResult(r), "mismatch") {
		t.Errorf("a failed check gives %q", e2sResult(r))
	}
	r = SResult{Hand: []HandCase{{Name: "x", Want: "a", Got: "b", Found: true}}}
	if r.Mismatches() != 1 {
		t.Error("a hand case that disagrees is not a mismatch")
	}
	r = SResult{NoTerminal: []int{5}}
	if r.Gaps() != 1 || !strings.HasPrefix(e2sResult(r), "incomplete") {
		t.Errorf("a gap gives %q", e2sResult(r))
	}
}

func TestHNF(t *testing.T) {
	// The rows (2, 1, 0), (0, 3, 0), (1, 1, 1) have the determinant 6 and span the lattice with the normal form (1, 0, 5), (0, 1, 2), (0, 0, 6): (2, 1, 0) = 2 (1, 0, 5) + (0, 1, 2) - 2 (0, 0, 6), (0, 3, 0) = 3 (0, 1, 2) - (0, 0, 6), (1, 1, 1) = (1, 0, 5) + (0, 1, 2) - (0, 0, 6).
	got := hnf3([3][3]int{{2, 1, 0}, {0, 3, 0}, {1, 1, 1}})
	if got != [3][3]int{{1, 0, 5}, {0, 1, 2}, {0, 0, 6}} {
		t.Errorf("the normal form is %v", got)
	}
	got = hnf3([3][3]int{{0, 0, -5}, {0, 7, 3}, {-1, 4, 2}})
	if got[0][0] != 1 || got[1][0] != 0 || got[2][0] != 0 || got[2][1] != 0 || got[0][0]*got[1][1]*got[2][2] != 35 {
		t.Errorf("the normal form is %v", got)
	}
}

func TestSimplicialInputs(t *testing.T) {
	for _, tc := range []struct {
		in   E2Inputs
		want string
	}{
		{E2Inputs{Dimension: 3, MaxIndex: 10, Family: "simplicial"}, ""},
		{E2Inputs{Dimension: 3, MaxIndex: 10, Family: "cyclic"}, ""},
		{E2Inputs{Dimension: 3, MaxIndex: 10, Family: "simplicial", ListIndex: 5}, ""},
		{E2Inputs{Dimension: 3, MaxIndex: 10, Family: "toric"}, "family"},
		{E2Inputs{Dimension: 4, MaxIndex: 10, Family: "simplicial"}, ""},
		{E2Inputs{Dimension: 4, MaxIndex: e2MaxIndexSimplicial4 + 1, Family: "simplicial"}, "maximum index"},
		{E2Inputs{Dimension: 5, MaxIndex: 10, Family: "simplicial"}, "dimension"},
		{E2Inputs{Dimension: 3, MaxIndex: e2MaxIndexSimplicial3 + 1, Family: "simplicial"}, "maximum index"},
		{E2Inputs{Dimension: 3, MaxIndex: 10, ListIndex: 5}, "family simplicial only"},
		{E2Inputs{Dimension: 3, MaxIndex: 10, Family: "simplicial", ListIndex: 11}, "list_index"},
	} {
		err := tc.in.check()
		if (tc.want == "") != (err == nil) || err != nil && !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%+v: the error is %v, want %q", tc.in, err, tc.want)
		}
	}
	if _, err := RunE2(E2Inputs{Dimension: 3, MaxIndex: 10, Family: "simplicial"}); err == nil {
		t.Error("RunE2 runs the family simplicial")
	}
}

const simplicialSpec = `name: e2-simplicial-small
experiment: E2
question: Do the small simplicial cases agree?
tool: mmp
node: MM00000001
inputs: {dimension: 3, max_index: 12, family: simplicial}
outputs: [germs.json, indices.json, summary.md]
notes: The notes of the run.
`

func TestRunSimplicial(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "lab", "specs", "e2-simplicial-small.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(simplicialSpec), 0o600); err != nil {
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
	for _, want := range []string{"## The checks", "| 1/2(1,1,0) |", "## Notes\n\nThe notes of the run.", "## What the run does not do", "| all |"} {
		if !strings.Contains(string(summary), want) {
			t.Errorf("the summary has no %q:\n%s", want, summary)
		}
	}
	germs, _ := os.ReadFile(filepath.Join(dir, "germs.json"))
	if !strings.HasPrefix(string(germs), "[\n{\"r\":2,") || strings.Count(string(germs), "\n") < 10 {
		t.Errorf("germs.json starts with %.80q", germs)
	}
}
