# Changelog

All changes that a user can see are in this file. The format follows Keep a Changelog, and the versions follow Semantic Versioning.

## Unreleased

### Added

- `mmp agent eval` runs the eval sets of spec 10.9 on a runner: the 50 statements of `eval/statements`, each as a faithful and a planted question, and the 60 definitions of `eval/audit`. It reads the verdict from the last `verdict:` line of the runner, sums the cost from `agent/ledger.jsonl`, and writes `records/eval/DATE-LABEL.json` with the pass rate of each set and the cost, and the answers in a `.jsonl`. With `-baseline`, it fails if a set drops by more than 5 points. The runner `constant:VERDICT` gives a baseline with no model, and `-limit` runs a trial with no record.
- The family `simplicial` of the lab experiment E2: with `family: simplicial` and `dimension: 3` in the inputs, `mmp lab run` lists every simplicial toric 3-fold germ up to the index `max_index`, by the Hermite normal forms of the dual lattices, once up to a permutation of the coordinates. For each germ it records the group, the mld at the fixed point with its minimizers, the mld of the germ, terminal, canonical, the divisors of log discrepancy at most 1, and enc in the old sense and in the sense of Han-Liu Definition 3.8(1). It checks that each terminal germ is cyclic of type 1/r(1, -1, a) with the mld 1 + 1/r, and that its cyclic isolated germs up to index 40 are those of the family `cyclic`, germ for germ. `list_index` bounds the germs that `germs.json` lists. The default family is `cyclic`, so an old spec gives the same result.

- The family `simplicial` of E2 in dimension 4: with `family: simplicial` and `dimension: 4`, `mmp lab run` lists every simplicial toric 4-fold germ up to the index `max_index`, by the Hermite normal forms of the dual lattices, once up to the 24 permutations of the coordinates. It records the same fields as in dimension 3, the invariant factors of groups of rank up to 3, and the Gorenstein index of each germ. It checks that its isolated cyclic germs up to index 30 are those of the family `cyclic` in dimension 4, and that its germs with a zero coordinate are the 3-fold germs of the family `simplicial` times a line, germ for germ, and it lists the terminal germs that are not of type 1/r(a, -a, b, -b). The maximum index is 300: index 200 took 136 s on 10 cores, and the work grows like r^4.

### Changed

- The maximum index of the family `simplicial` of E2 is 1200, up from 1000: index 1000 ran in 12.4 min, and the work grows like r^4.

### Fixed

- The family `cyclic` of E2 counts `at_most_one` as the primitive points of the box of age at most 1, the exceptional divisors of log discrepancy at most 1. It counted every point of the box, and a point such as (2, 2, 2)/7 of 1/7(1, 1, 1) is twice a lattice point and is not a divisor. So `enc` changes for some germs. The family `simplicial` now checks this count against the family `cyclic` germ for germ, and its summary no longer has the note on the old count.
- The trust report finds the toric cone theorem. Its top theorem is `MinimalModel.Toric.toricConeTheorem`, which proves the pinned statement `MinimalModel.Toric.ToricConeTheorem`. The name `MinimalModel.Toric.coneTheorem` of spec 14.2 does not exist in mmp-lean.

## v0.0.4 - 2026-11-26

### Added

- `mmp agent cost` measures the cost per proved node and per audited definition of milestone S2 from the ledger, the run records, the map and `records/audit.json`. It shows the spend by role and by route, the spend on nodes that are not proved yet, and the calls whose run has no record. With `-write` it writes `reports/cost-<to>.md`.

## v0.0.3 - 2026-11-26

## v0.0.2 - 2026-11-26

### Fixed

- A node of kind `claim` with no Lean statement has the status `unstated`, as spec 08 says. Only a claim with a Lean statement is `claimed`. In tamnd/mmp the nodes of S27, S47 and S57 move from `claimed` to `unstated`.

## v0.0.1 - 2026-11-26

### Added

- The first version of the toolchain `mmp`, from the toolchain of the sibling project langlands. It has the commands `check`, `status`, `index`, `port check`, `fidelity`, `trust`, `paper`, `lab`, `claim`, `queue`, `agent`, `map`, `site build` and `site serve`.
- The status `proved-ext` of spec 06.4: a node with a bridge theorem that builds clean, when the audit of the external library has passed. While the audit is open, such a node is `claimed`.
- `records/bridge.json` and `records/audit.json` in the project, and the trust rows Q9 (the audit) and Q10 (the answer of the authors of a claim).
- The tiers T0 to T11 of spec 07, the research routes R1 to R9 of spec 04.1, the role `auditor` of spec 10.3, and the top theorems of spec 14.2.
- The `claim` block of spec 07.14 in a paper record, with the fields `first_seen`, `checked_part`, `unrefereed_inputs`, `authors_contacted` and `lean_result`. `mmp check` checks the dates and the value of `lean_result`, and it refuses the expert status `accepted` in a record with a claim block.
- The pages `/statements/` and `/audit/` in the menu of the site.
- The lab experiment E2 of spec 13.3: the mlds of the isolated cyclic quotient germs of dimension 3 and 4. In dimension 3 it checks the terminal lemma and the mld 1 + 1/r of a terminal germ.

- `mmp audit status`, `mmp audit step` and `mmp audit finding` read and write `records/audit.json`, the state of the audit of spec 04.3. A step is `done` only when it has an exit record in `audit/`.
- `mmp audit axioms` runs `#print axioms` on the seven declarations of step A2 in a built checkout of AxiomMath/MMP and writes the axiom report.
- The page `/audit/` shows the steps A1 to A8, their exit records and the open findings from `records/audit.json`, and the site writes `/api/audit.json`.

### Fixed

- The statements of the top theorems match the table of spec 06.2. The theorem `MinimalModel.Surface.minimalModel` proves `MinimalModel.SurfaceMMP` (S12). The toric cone theorem, toric flips, their termination, the lemma O-R4-1 and the toric case of S52 have no statement in spec 06.2, so their statement is empty.
- `mmp lab run` does not write the name of the machine in the record. The default host is the operating system and the architecture, for example `darwin/arm64`.
