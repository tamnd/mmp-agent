package lab

import (
	"errors"
	"fmt"
	"sort"
)

// E2Inputs are the inputs of experiment E2, the minimal log discrepancies of toric germs. The family cyclic, the default, covers the isolated cyclic quotient germs 1/r(a_1, ..., a_n) of one dimension n, for each index r from 2 to the maximum. The family simplicial covers each simplicial toric germ of dimension 3, whose index is the order of its group (e2s.go).
type E2Inputs struct {
	Dimension int    `yaml:"dimension" json:"dimension"`                       // 3 or 4
	MaxIndex  int    `yaml:"max_index" json:"max_index"`                       // the largest index r
	Family    string `yaml:"family,omitempty" json:"family,omitempty"`         // cyclic or simplicial; empty is cyclic
	ListIndex int    `yaml:"list_index,omitempty" json:"list_index,omitempty"` // simplicial: germs.json lists the germs with r <= list_index; 0 is max_index
}

// Simplicial says whether the inputs are of the family simplicial.
func (in E2Inputs) Simplicial() bool { return in.Family == "simplicial" }

func (in E2Inputs) listIndex() int {
	if in.ListIndex == 0 {
		return in.MaxIndex
	}
	return in.ListIndex
}

// The limits keep a run inside the budget of spec 13.6 on one machine.
const (
	e2MaxIndex3 = 200
	e2MaxIndex4 = 60
)

func (in E2Inputs) check() error {
	switch {
	case in.Family != "" && in.Family != "cyclic" && in.Family != "simplicial":
		return fmt.Errorf("E2: the family is %q, but mmp runs only the families cyclic and simplicial", in.Family)
	case in.Simplicial() && in.Dimension != 3:
		return fmt.Errorf("E2: the family simplicial has only the dimension 3, not %d", in.Dimension)
	case in.Simplicial() && in.MaxIndex > e2MaxIndexSimplicial3:
		return fmt.Errorf("E2: the maximum index of the family simplicial is %d", e2MaxIndexSimplicial3)
	case !in.Simplicial() && in.ListIndex != 0:
		return errors.New("E2: list_index is an input of the family simplicial only")
	case in.ListIndex < 0 || in.ListIndex > in.MaxIndex:
		return fmt.Errorf("E2: list_index is %d, but it must be between 0 and max_index", in.ListIndex)
	case in.Dimension != 3 && in.Dimension != 4:
		return fmt.Errorf("E2: the dimension is %d, but mmp runs only the dimensions 3 and 4", in.Dimension)
	case in.MaxIndex < 2:
		return errors.New("E2: the maximum index must be at least 2")
	case !in.Simplicial() && in.Dimension == 3 && in.MaxIndex > e2MaxIndex3:
		return fmt.Errorf("E2: the maximum index in dimension 3 is %d", e2MaxIndex3)
	case in.Dimension == 4 && in.MaxIndex > e2MaxIndex4:
		return fmt.Errorf("E2: the maximum index in dimension 4 is %d", e2MaxIndex4)
	}
	return nil
}

// Germ is one isolated cyclic quotient germ 1/r(a_1, ..., a_n) in its normal form: the weights are sorted, and they are the smallest sorted weights over all the generators of the group.
//
// The germ is the affine toric variety of the simplicial cone spanned by e_1, ..., e_n in the lattice Z^n + Z (a_1, ..., a_n)/r. The log discrepancy of the divisor of a primitive vector v of the cone is the sum of the coordinates of v. Because the germ is isolated, each point of the box, (k a_1 mod r, ..., k a_n mod r)/r for k from 1 to r-1, is in the interior of the cone, and its log discrepancy is the age of k. So the mld at the origin is the minimum of n and of the ages.
type Germ struct {
	Index    int   `json:"r"`
	Weights  []int `json:"weights"`
	MldNum   int   `json:"mld_num"` // the mld is MldNum / MldDen, in lowest terms
	MldDen   int   `json:"mld_den"`
	MinAgeK  int   `json:"min_age_k"`                // the smallest k of minimal age, or 0 when the mld is n
	Terminal bool  `json:"terminal"`                 // each age is more than 1
	Canon    bool  `json:"canonical"`                // each age is at least 1
	AtMost1  int   `json:"at_most_one"`              // the number of box points of age at most 1: the exceptional divisors of log discrepancy at most 1
	Lemma    *bool `json:"terminal_lemma,omitempty"` // dimension 3: two weights add to 0 mod r
}

// E2Index is the count of the germs of one index.
type E2Index struct {
	Index     int `json:"r"`
	Germs     int `json:"germs"`
	Terminal  int `json:"terminal"`
	Canonical int `json:"canonical"`
	Enc       int `json:"enc"` // the germs with exactly one exceptional divisor of log discrepancy at most 1
}

// E2Result is the result of a run of E2.
type E2Result struct {
	Dimension int       `json:"dimension"`
	Germs     []Germ    `json:"germs"`
	Indices   []E2Index `json:"indices"`
	// The checks of dimension 3. LemmaFails are the germs where the terminal lemma and the ages disagree. MldFails are the terminal germs whose mld is not 1 + 1/r. NoTerminal are the indices with no terminal germ, although 1/r(1, r-1, 1) is terminal.
	LemmaFails []Germ `json:"lemma_fails"`
	MldFails   []Germ `json:"mld_fails"`
	NoTerminal []int  `json:"no_terminal"`
	// Values are the distinct mlds less than 1, smallest first, as "p/q".
	Values []string `json:"values_below_one"`
}

// Mismatches is the number of outputs that contradict a prediction.
func (r E2Result) Mismatches() int { return len(r.LemmaFails) + len(r.MldFails) }

// Gaps is the number of indices where a predicted germ is missing.
func (r E2Result) Gaps() int { return len(r.NoTerminal) }

func gcd(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	if a < 0 {
		return -a
	}
	return a
}

func units(r int) []int {
	var u []int
	for a := 1; a < r; a++ {
		if gcd(a, r) == 1 {
			u = append(u, a)
		}
	}
	return u
}

// normalForm returns the smallest sorted tuple of u*w mod r over the units u.
func normalForm(w []int, r int, us []int) []int {
	var best []int
	cur := make([]int, len(w))
	for _, u := range us {
		for i, a := range w {
			cur[i] = u * a % r
		}
		if len(cur) == 3 {
			sort3(cur)
		} else {
			sort.Ints(cur)
		}
		if best == nil || less(cur, best) {
			best = append(best[:0], cur...)
		}
	}
	return best
}

func sort3(a []int) {
	if a[0] > a[1] {
		a[0], a[1] = a[1], a[0]
	}
	if a[1] > a[2] {
		a[1], a[2] = a[2], a[1]
	}
	if a[0] > a[1] {
		a[0], a[1] = a[1], a[0]
	}
}

func less(a, b []int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

func key(w []int) string { return fmt.Sprint(w) }

// germ computes the ages of a germ.
func germ(w []int, r int) Germ {
	n := len(w)
	g := Germ{Index: r, Weights: append([]int(nil), w...), Terminal: true, Canon: true}
	minAge, minK := n*r, 0 // the ages are counted in units of 1/r
	for k := 1; k < r; k++ {
		age := 0
		for _, a := range w {
			age += k * a % r
		}
		if age < minAge {
			minAge, minK = age, k
		}
		if age <= r {
			g.Terminal = false
			g.AtMost1++
		}
		if age < r {
			g.Canon = false
		}
	}
	d := gcd(minAge, r)
	g.MldNum, g.MldDen, g.MinAgeK = minAge/d, r/d, minK
	if n == 3 {
		lemma := (w[0]+w[1])%r == 0 || (w[0]+w[2])%r == 0 || (w[1]+w[2])%r == 0
		g.Lemma = &lemma
	}
	return g
}

// RunE2 enumerates the germs of the family cyclic and checks the predictions. In dimension 3 the predictions are the terminal lemma of Morrison-Stevens (an isolated cyclic quotient 3-fold germ is terminal if and only if two of its weights add to 0 mod r) and the mld 1 + 1/r of a terminal germ. Dimension 4 has no prediction: the run records the counts and the values for Target T (spec 04.12).
func RunE2(in E2Inputs) (E2Result, error) {
	if err := in.check(); err != nil {
		return E2Result{}, err
	}
	if in.Simplicial() {
		return E2Result{}, errors.New("E2: RunE2 runs the family cyclic; use RunE2Simplicial")
	}
	n := in.Dimension
	res := E2Result{Dimension: n, Germs: []Germ{}, LemmaFails: []Germ{}, MldFails: []Germ{}, NoTerminal: []int{}}
	values := map[[2]int]bool{}
	for r := 2; r <= in.MaxIndex; r++ {
		us := units(r)
		seen := map[string]bool{}
		row := E2Index{Index: r}
		// Each class has a member with the weight 1, so the first weight is 1 and the others are sorted units.
		w := make([]int, n)
		w[0] = 1
		var walk func(i, from int)
		walk = func(i, from int) {
			if i == n {
				nf := normalForm(w, r, us)
				k := key(nf)
				if seen[k] {
					return
				}
				seen[k] = true
				g := germ(nf, r)
				res.Germs = append(res.Germs, g)
				row.Germs++
				if g.Terminal {
					row.Terminal++
				}
				if g.Canon {
					row.Canonical++
				}
				if g.AtMost1 == 1 {
					row.Enc++
				}
				if g.Lemma != nil && *g.Lemma != g.Terminal {
					res.LemmaFails = append(res.LemmaFails, g)
				}
				if n == 3 && g.Terminal && (g.MldNum != r+1 || g.MldDen != r) {
					res.MldFails = append(res.MldFails, g)
				}
				if g.MldNum < g.MldDen {
					values[[2]int{g.MldNum, g.MldDen}] = true
				}
				return
			}
			for j := from; j < len(us); j++ {
				w[i] = us[j]
				walk(i+1, j)
			}
		}
		walk(1, 0)
		if n == 3 && row.Terminal == 0 {
			res.NoTerminal = append(res.NoTerminal, r)
		}
		res.Indices = append(res.Indices, row)
	}
	vs := make([][2]int, 0, len(values))
	for v := range values {
		vs = append(vs, v)
	}
	sort.Slice(vs, func(i, j int) bool { return vs[i][0]*vs[j][1] < vs[j][0]*vs[i][1] })
	res.Values = make([]string, len(vs))
	for i, v := range vs {
		res.Values[i] = fmt.Sprintf("%d/%d", v[0], v[1])
	}
	return res, nil
}
