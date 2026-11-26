package blueprint

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, root, name, text string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func fixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write(t, root, "blueprint/routes.yaml", "- id: R1\n  title: Polyhedra\n  weight: 0.25\n  idea: i\n  obstruction: o\n  known: k\n  first: f\n")
	write(t, root, "blueprint/nodes/MM00000001.yaml", "tag: MM00000001\nkind: definition\ntitle: Regular\nstatement: A regular scheme.\ntier: S\nlean:\n  statement: MinimalModel.IsRegularScheme\n")
	write(t, root, "blueprint/nodes/MM00000002.yaml", "tag: MM00000002\nkind: theorem\ntitle: Smooth points\nstatement: A smooth point is terminal.\ntier: S\nuses: [MM00000001]\nlean:\n  statement: MinimalModel.LocalReciprocityGL1\n  proof: MinimalModel.localReciprocityGL1_unramified\nsources:\n  - paper: Ser67\n")
	write(t, root, "blueprint/nodes/MM00000003.yaml", "tag: MM00000003\nkind: theorem\ntitle: Toric cones\nstatement: The toric cone theorem.\ntier: T5\nuses: [MM00000004]\nroute: [R1]\nlean:\n  statement: MinimalModel.LocalReciprocityGL1\n  proof: MinimalModel.localReciprocityGL1\n")
	write(t, root, "blueprint/nodes/MM00000004.yaml", "tag: MM00000004\nkind: hypothesis\ntitle: Normalization\nstatement: Finite.\ntier: T1\nlean:\n  statement: MinimalModel.Normalization\n")
	write(t, root, "blueprint/nodes/MM00000005.yaml", "tag: MM00000005\nkind: conjecture\ntitle: Dimension 4\nstatement: Open.\ntier: S\nlean:\n  statement: MinimalModel.ResolutionDimFourCharP\n")
	write(t, root, "papers/Ser67.yaml", "id: T0-04\nkey: Ser67\ntitle: Local class field theory\nread: {version: book, by: owner, date: 2026-11-25, tag: S}\ntier: T3\nwhy: local CFT\nexpert_status: accepted\nlean_status: port-available\n")
	write(t, root, "records/lean.json", `{"commit":"abc","decls":{
"MinimalModel.IsRegularScheme":{"kind":"def","axioms":["propext"]},
"MinimalModel.LocalReciprocityGL1":{"kind":"def","axioms":[]},
"MinimalModel.localReciprocityGL1_unramified":{"kind":"theorem","axioms":["propext","Classical.choice","Quot.sound"]},
"MinimalModel.localReciprocityGL1":{"kind":"theorem","axioms":["propext"]},
"MinimalModel.Normalization":{"kind":"def","axioms":[]},
"MinimalModel.ResolutionDimFourCharP":{"kind":"def","axioms":[]}}}`)
	return root
}

func TestLoadAndStatus(t *testing.T) {
	p, err := Load(fixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if probs := p.Check(); len(probs) != 0 {
		t.Fatalf("problems: %v", probs)
	}
	want := map[string]string{
		"MM00000001": Defined,
		"MM00000002": Proved,
		"MM00000003": ProvedMod,
		"MM00000004": Stated,
		"MM00000005": Open,
	}
	for tag, st := range want {
		if got := p.Node(tag).Status; got != st {
			t.Errorf("%s: status %s, want %s (%s)", tag, got, st, p.Node(tag).Why)
		}
	}
	if got := p.Paper("Ser67").Nodes; len(got) != 1 || got[0] != "MM00000002" {
		t.Errorf("Ser67 nodes = %v", got)
	}
	if got := p.Node("MM00000001").UsedBy; len(got) != 1 {
		t.Errorf("used by = %v", got)
	}
}

func TestSorryIsNotAProof(t *testing.T) {
	root := fixture(t)
	write(t, root, "records/lean.json", `{"decls":{
"MinimalModel.IsRegularScheme":{"kind":"def","axioms":[]},
"MinimalModel.LocalReciprocityGL1":{"kind":"def","axioms":[]},
"MinimalModel.localReciprocityGL1_unramified":{"kind":"theorem","axioms":["sorryAx"]},
"MinimalModel.localReciprocityGL1":{"kind":"theorem","axioms":["propext","MinimalModel.myAxiom"]},
"MinimalModel.Normalization":{"kind":"def","axioms":[]},
"MinimalModel.ResolutionDimFourCharP":{"kind":"def","axioms":[]}}}`)
	p, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Node("MM00000002").Status; got != Stated {
		t.Errorf("a proof with sorry has status %s", got)
	}
	if got := p.Node("MM00000003").Status; got != Stated {
		t.Errorf("a proof with a new axiom has status %s", got)
	}
}

func TestCheckFindsFaults(t *testing.T) {
	root := fixture(t)
	write(t, root, "blueprint/nodes/MM00000006.yaml", "tag: MM00000006\nkind: guess\ntitle: Bad\nstatement: x\ntier: T12\nuses: [MM00000007, MM00000006]\nsources:\n  - paper: Nobody\nroute: [R7]\n")
	write(t, root, "blueprint/nodes/SGbad.yaml", "tag: SGbad\nkind: lemma\ntitle: t\nstatement: s\ntier: T1\n")
	p, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	probs := strings.Join(p.Check(), "\n")
	for _, want := range []string{"kind \"guess\"", "tier \"T12\"", "MM00000007, which is not a node", "Nobody", "route \"R7\"", "cycle", "SGbad"} {
		if !strings.Contains(probs, want) {
			t.Errorf("the check does not report %q:\n%s", want, probs)
		}
	}
}

func TestUnknownFieldIsAnError(t *testing.T) {
	root := fixture(t)
	write(t, root, "papers/Bad.yaml", "id: T1-01\nkey: Bad\ntitle: t\ntier: T1\nwhy: w\nstatus: proved\n")
	if _, err := Load(root); err == nil {
		t.Fatal("a paper record with an unknown field loads with no error")
	}
}

func TestPlannedNameIsNotAFault(t *testing.T) {
	root := fixture(t)
	write(t, root, "blueprint/nodes/MM00000006.yaml", "tag: MM00000006\nkind: theorem\ntitle: Toric flips\nstatement: Toric flips exist.\ntier: T5\nlean:\n  statement: MinimalModel.LocalReciprocityGL1\n  proof: MinimalModel.localLanglandsGL2\n")
	write(t, root, "blueprint/nodes/MM00000007.yaml", "tag: MM00000007\nkind: lemma\ntitle: Open locus\nstatement: Open.\ntier: T1\nlean:\n  statement: MinimalModel.regularLocus_isOpen\n")
	write(t, root, "papers/Tat67.yaml", "id: T3-02\nkey: Tat67\ntitle: Fourier analysis in number fields and Hecke zeta functions\nread: {version: book, by: owner, date: 2026-11-25, tag: P}\ntier: T3\nwhy: Tate thesis\nexpert_status: accepted\nlean_status: none\nlean_elsewhere:\n  - {repo: someone/riemann-lean, licence: none, what: Tate thesis, use: read}\n")
	p, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if probs := p.Check(); len(probs) != 0 {
		t.Fatalf("problems: %v", probs)
	}
	if got := p.Node("MM00000006").Status; got != Stated {
		t.Errorf("a statement with no proof in the records has status %s", got)
	}
	if got := p.Node("MM00000007").Status; got != Planned {
		t.Errorf("a name that is not in the records has status %s", got)
	}
}

func TestPaperRecordFaults(t *testing.T) {
	root := fixture(t)
	write(t, root, "papers/Bad1.yaml", "id: X-1\nkey: Bad1\ntitle: t\nread: {version: v1, by: reader, date: 2026-11-25, tag: Q}\ntier: T1\nwhy: w\nexpert_status: maybe\nlean_status: some\nalso: [T99]\nresults:\n  - {id: \"Theorem 1\", statement: X, node: MM0000000Z, kind: lemma, status: stated}\nlean_elsewhere:\n  - {repo: a/b, licence: MIT, what: all, use: depend}\n")
	p, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	probs := strings.Join(p.Check(), "\n")
	for _, want := range []string{"the id \"X-1\"", "read tag \"Q\"", "expert status \"maybe\"", "lean status \"some\"", "\"T99\"", "kind \"lemma\"", "node MM0000000Z", "use \"depend\""} {
		if !strings.Contains(probs, want) {
			t.Errorf("the check does not report %q:\n%s", want, probs)
		}
	}
}

func TestPaperClaimBlock(t *testing.T) {
	root := fixture(t)
	write(t, root, "papers/Good2.yaml", "id: T8-12\nkey: Good2\ntitle: t\nread: {version: v1, by: reader, date: 2026-11-25, tag: S}\ntier: T8\nwhy: w\nexpert_status: claimed\nlean_status: none\nclaim:\n  first_seen: 2026-10-14\n  checked_part: the example\n  unrefereed_inputs: []\n  authors_contacted: null\n  lean_result: null\n")
	write(t, root, "papers/Bad2.yaml", "id: T8-13\nkey: Bad2\ntitle: t\nread: {version: v1, by: reader, date: 2026-11-25, tag: S}\ntier: T8\nwhy: w\nexpert_status: accepted\nlean_status: none\nclaim:\n  first_seen: soon\n  authors_contacted: later\n  lean_result: fine\n")
	p, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	probs := strings.Join(p.Check(), "\n")
	if strings.Contains(probs, "Good2") {
		t.Errorf("the check reports a fault in a good claim block:\n%s", probs)
	}
	for _, want := range []string{"first_seen as a date", "authors_contacted \"later\"", "lean result \"fine\"", "cannot have the expert status accepted"} {
		if !strings.Contains(probs, want) {
			t.Errorf("the check does not report %q:\n%s", want, probs)
		}
	}
}

func TestFaithfulStatus(t *testing.T) {
	root := fixture(t)
	write(t, root, "blueprint/nodes/MM00000006.yaml", "tag: MM00000006\nkind: theorem\ntitle: Modularity\nstatement: Modularity over Q.\ntier: T8\nlean:\n  statement: MinimalModel.ModularityEllipticQ\n")
	write(t, root, "blueprint/nodes/MM00000007.yaml", "tag: MM00000007\nkind: conjecture\ntitle: Open\nstatement: Open.\ntier: T8\nlean:\n  statement: MinimalModel.ResolutionDimFourCharP\n")
	write(t, root, "records/lean.json", `{"commit":"abc","decls":{"MinimalModel.ModularityEllipticQ":{"kind":"def","axioms":[]},"MinimalModel.ResolutionDimFourCharP":{"kind":"def","axioms":[]}}}`)
	write(t, root, "records/fidelity.json", `{"MinimalModel.ModularityEllipticQ":{"class":"theorem","checks":{"F1":true,"F2":true,"F3":true},"pass":true},"MinimalModel.ResolutionDimFourCharP":{"class":"conjecture","checks":{"F1":true},"pass":true}}`)
	p, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Node("MM00000006").Status; got != Faithful {
		t.Errorf("a statement that passed the fidelity checks has status %s", got)
	}
	if got := p.Node("MM00000007").Status; got != Open {
		t.Errorf("a conjecture that passed the fidelity checks has status %s, not open", got)
	}
}
