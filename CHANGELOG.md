# Changelog

All changes that a user can see are in this file. The format follows Keep a Changelog, and the versions follow Semantic Versioning.

## Unreleased

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
