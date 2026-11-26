# mmp-agent

The toolchain of [mmp](https://github.com/tamnd/mmp), the public project that states the minimal model program in Lean 4, proves its known parts, audits the external Lean proofs of the program and works on the open parts. The command is `mmp`. It checks the map of the content repository, gives the status of each node from the record of the Lean build, and builds the site [mmp.tamnd.com](https://mmp.tamnd.com).

Termination of flips and abundance are open in dimension 4 and higher. This toolchain does not decide that a result is proved. The Lean kernel decides that, and `mmp` only reads the record of the build. For a theorem of an external library, such as AxiomMath/MMP, `mmp` also reads the record of the audit. A bridge that builds is not enough.

## Install

You need Go 1.27 or later.

```sh
go install github.com/tamnd/mmp-agent/cmd/mmp@latest
```

Each release on GitHub also has binaries for Linux, macOS and Windows, with checksums and a build provenance attestation.

## Use

Run the commands in a clone of `tamnd/mmp`.

```sh
mmp check                      # check the map: tags, links, papers, routes and Lean names
mmp status                     # print the status of each node
mmp status -only stated        # print only the stated nodes
mmp index -lean lean.json      # copy the record of the Lean build to records/lean.json
mmp port check -lean ../mmp-lean   # check the ported Lean files against manifests/ported.yaml
mmp fidelity list              # the fidelity checks of each statement, from records/fidelity.json
mmp fidelity show MMWK2DDDPC   # the checks of one statement, by node tag or Lean name
mmp fidelity set MinimalModel.ConeTheoremKlt F4=true   # record a check that a person or a role did
mmp fidelity run -all          # compute F1 from records/lean.json for each statement
mmp trust -lean ../mmp-lean    # write records/trust.json and print the answers Q0 to Q10
mmp audit status               # the steps A1 to A8 of the audit of AxiomMath/MMP, with their exit records
mmp audit step A1 done         # set the state of a step in records/audit.json
mmp audit axioms -source ../MMP -json audit/axiom-mmp/A2.json   # step A2: #print axioms on the seven declarations
mmp paper list -tier T5        # the paper records of papers/, with their tier and status
mmp paper show Rei83           # one paper record, with the status of its results from the map
mmp paper add -key FS04 -id T5-04 -title "..." -tier T5 -why "..." -expert-status accepted 10.2748/tmj/1113246736
mmp lab list                   # the experiments of spec 13.3, the specs of lab/specs and the runs of lab/runs
mmp lab spec E2 > lab/specs/e2-cyclic-quotient-mld-3.yaml   # the spec of E2
mmp lab run E2                 # run the spec of E2 and write lab/runs/<id>/
mmp lab show <run id>          # the record and the summary of a run
mmp claim add -date 2026-11-26 arXiv:2610.09270   # write a fresh claim record claims/arXiv-2610-09270.yaml
mmp claim list                 # the claim records of claims/, with their status and parts
mmp claim show arXiv-2610-09270   # one claim record
mmp claim plan arXiv-2610-09270   # check the record and print its parts, with the statements and the cost
mmp queue -role prover -n 20   # the work queue of a role, with the rank and the reason of each item
mmp agent run prover -dry-run  # print the run record and the prompt of the top item, and write nothing
mmp agent run prover -node MMAWP4SA44 -budget 5   # write records/runs/<id>/ and start the runner, if there is one
mmp agent report -week 2026-W49   # write reports/2026-W49.md
mmp agent cost -from 2026-11-01 -to 2026-11-30   # the cost per proved node and per audited definition; -write keeps it in reports/cost-2026-11-30.md
mmp site build -content . -out public
mmp site serve -dir public     # serve the site at http://127.0.0.1:8080/
```

The exit codes are 0 for success, 1 for a failed check or a failed run, 2 for a usage error and 3 for a command that is not written yet. The help text gives the milestone of each part that is not written yet. The evaluation sets of spec 10.9 come in S2.

## The claim records

`mmp claim add` writes a new `claims/<key>.yaml` for a fresh claim of route R1. The argument is an arXiv id, `arXiv:` and an id, an arXiv address, or another http or https address. The key is stable: all versions of an arXiv preprint give the same key, for example `arXiv-2611-31396`, and another address gives `url-` and the first 10 hex digits of its sha256. You can set the key with `-key`. The record has the source, the date (`-date`, default today), the status `fresh` and no parts. The command does not fetch the preprint, and it never overwrites a file. The reader role proposes the self-contained parts of a claim, and a person accepts each one. A part has an id, a title, the place in the preprint, a statement, the parts and nodes that it uses, the named hypotheses, a cost from 1 to 5 and a status. `mmp claim plan` checks the record and prints each part with its statement and cost, and it says whether the part is self-contained: each part or node that it uses is another part, or a node that is proved or defined. `mmp check` checks the claim records too.

## The work queue

`mmp queue` ranks the nodes that a role can act on with the formula of spec 10.4: rank = weight x (1 + unblocks) x (1 + level bonus) / (1 + failed runs) x age factor. The terms are these:

- The weight of a route node is the weight of its route in `blueprint/routes.yaml`. The weight of a library node is the weight of the first milestone that needs it, from `blueprint/milestones.yaml`. A node that no milestone needs has the weight `other`.
- `unblocks` is the number of the nodes that use the node and wait only on it.
- The level bonus is 1 for a node on the path to a top theorem of the current milestone. The current milestone is the first milestone of spec 14.2 with a top theorem that is not proved, or the milestone of `-milestone`.
- The failed runs are the run records of the role on the node with the status `failed`.
- The age factor is 2 when the role has no run on the node. Otherwise it is 1 plus the days since the last run, up to 28, divided by 28.

When `blueprint/routes.yaml` or `blueprint/milestones.yaml` is missing, `mmp queue` writes it with the default weights: the weights of spec 04.1 for the routes, and 1 for S0 to S2, 0.8 for S3, 0.6 for S4, 0.5 for S5, 0.4 for S6, 0.3 for S7, 0.2 for S8, 0.1 for S9 and 0.1 for `other`. The milestone weights are a guess of the project, because spec 10.4 gives no numbers. People set the weights. The roles reader (claims), stater, prover, refuter, explorer, explainer and linker (nodes) have a queue. The output is the same for the same files and the same `-date`.

## The agent

`mmp agent run <role>` takes the top item of the queue of the role, or the node of `-node`, or the claim of `-claim`. It builds the prompt from the role table of spec 10.3, the node, the nodes that it uses, its papers, the Lean declarations of the same file and the output of the failed runs. Text from the map, the papers, a claim or an earlier run goes in a marked data block, and the prompt says that instructions in a data block are ignored (spec 10.7). The model comes from `agent.yaml`, or from the model table of spec 10.6 when there is no `agent.yaml`. The budget is the value of `-budget`, lowered to the run limit, to what is left of the day limit and to what is left of the day limit of the role, from `agent/ledger.jsonl`. With no `agent.yaml` the budget is zero.

The command writes `records/runs/<id>/run.json` with the status `planned`, the model, the budget, the inputs and the branch, and `prompt.md` with the prompt. Then it prints the path of the record. With `-dry-run` it prints the record and the prompt and writes nothing. When the budget is zero, or when the environment variable `MMP_RUNNER` is not set, the run stops after the record and the command says why. When `MMP_RUNNER` is set, the command runs it in the content repository with the prompt on stdin and with `LGL_RUN_ID`, `LGL_ROLE`, `LGL_TARGET`, `LGL_MODEL`, `LGL_BUDGET`, `LGL_BRANCH`, `LGL_RUN_DIR` and `LGL_RECORD` in the environment. The output goes to `output.log`, and the record gets the exit status. An exit status of 0 gives the status `ran`, and the result waits for the checks of the role. Another exit status gives `failed`, and the command exits with 1. mmp calls no model itself, it makes no worktree or branch, and it opens no pull request. The runner writes the ledger.

`mmp agent report -week 2026-W49` writes `reports/2026-W49.md` with the sections of spec 10.8: the status changes by tier and by route, the pull requests of the run records, the spend by role and by route from the ledger, the lab runs, the claim checks, the refuted conjectures and the five largest open nodes of each route and of the library, with what blocks them. To find the status changes, it writes the status of each node to `records/status/<week>.json` and compares it with the last snapshot of an earlier week.

`mmp agent cost` is the measurement of milestone S2: the cost per proved node and per audited definition. It prints a Markdown report on stdout. The window runs from `-from` (default: the first entry of the ledger) to `-to` (default: today, UTC), and a call counts on its date in `agent/ledger.jsonl`, or on the date of its run when it has none. The report gives the spend by role and by route, and a table that puts each call in one row: runs on a node that is proved now, runs on a node that is not proved yet, runs on a claim, runs on a target that is not in the map, and unattributed calls, whose run has no record. The cost per proved node is the spend on the nodes whose status is `proved` now, divided by the number of these nodes, and the spend on the other nodes is listed as work in progress. The cost per audited definition is the spend of the role auditor divided by the number of audited definitions. `records/audit.json` keeps only the state of the steps A1 to A8 and the open findings, so this number is 0 and the report says that the cost is not defined yet. A call with no date is in no window, and the report names it on its own line. With no ledger the report says that no spend is recorded, and the command exits with 0. With `-write` it also writes `reports/cost-<to>.md`.

## The status rules

The status of a node comes from `records/lean.json`, from `records/fidelity.json` and from the nodes that it uses. The rules are in `blueprint/blueprint.go`.

| Status | When |
|---|---|
| unstated | The node has no Lean statement in the build. |
| stated | The statement builds, but the proof uses `sorry` or an axiom that is not standard. |
| faithful | The statement builds and passed the fidelity checks of its class in `records/fidelity.json` (spec 06.3). There is no proof yet. |
| defined | A definition that builds with no `sorry`. |
| proved | The proof uses only `propext`, `Classical.choice` and `Quot.sound`, and each node that it uses is proved or defined. |
| proved-mod | The proof is clean but it takes named hypotheses, or a node that it uses is not proved. |
| proved-ext | The node has a bridge theorem in `MinimalModelBridge` that builds clean, and the audit of the external library in `records/audit.json` has passed. |
| open | A conjecture. No proof is claimed. |
| claimed | A fresh claim of route R1, or a node whose bridge builds while the audit of the external library has not passed. It is never a hypothesis of another node. |
| refuted | The negation is proved. |

## The site

The site uses the style and the math renderer of [Math 2.0](https://github.com/tamnd/math2-agent). The math is rendered to MathML at build time, so a page shows its math with JavaScript off. Each page has a Markdown copy next to it, and `/api/` has the map, the papers and the trust report as JSON. The page `/lab/` lists the runs of `lab/runs/`, and each run has a page with its summary and its files.

## Develop

```sh
make check      # gofmt, vet, the tests with the race detector, and golangci-lint
```

Read [CONTRIBUTING.md](CONTRIBUTING.md) before you open a pull request.

## Licence

MIT. See [LICENSE](LICENSE). The files in `site/assets/` come from Math 2.0. See [site/assets/UPSTREAM.md](site/assets/UPSTREAM.md) for their sources and licences.

## The port check

`mmp port check` reads `manifests/ported.yaml` of mmp-lean and the files of `MinimalModel/Ported/`. It finds these problems:

- an entry with no source, a repository that is not on GitHub, a commit that is not 40 hex digits, a licence that is not Apache-2.0, MIT or BSD-3-Clause, or a date that is not `YYYY-MM-DD`;
- a ported Lean file that does not start with the licence header of its source;
- a ported Lean file with no provenance line `Ported from <owner>/<repo> at commit <commit>` in its first 40 lines;
- a Lean file of `MinimalModel/Ported/` that is in no entry, or in two entries.

## The fidelity checks

`mmp fidelity` keeps `records/fidelity.json`, the results of the checks F1 to F7 of spec 06.3. It computes `pass` from the class of the statement and its checks:

| Class | Needs |
|---|---|
| theorem | F1 to F5. A statement that a top theorem of spec 14.2 proves also needs F6. |
| open | F1 to F7. The two reviewers of F6 must be from two groups. |
| claimed | F1 to F7, as for open. |

F1 is computed from `records/lean.json`: the declaration is in the record, it has no `sorry`, and it uses the standard axioms only. A person or a role sets F2 to F7 with `mmp fidelity set`. F6 needs two reviewers, given with `-reviewer "name|group|YYYY-MM-DD"`. `mmp check` reports an entry that says `pass` but does not have the checks of its class.

## The trust report

`mmp trust` writes `records/trust.json`, the answers to the questions Q0 to Q10 of spec 14, and prints them as text. It does not build Lean and it does not run a checker. It reads `records/lean.json`, `records/fidelity.json`, the map and, if they exist, a checkout of mmp-lean and the record of the kernel replay. A row that needs a person, a build or a replay that mmp did not see is `not run` or `open`, never `yes`. The command exits with 1 when a row is `no`.

| Row | How `mmp trust` answers it |
|---|---|
| Q0 | With `-lean`, it compares the commit of the checkout with the commit of `records/lean.json`. |
| Q1 | It reports the commit, the toolchain and the date of the build in `records/lean.json`. |
| Q2 | It checks each declaration that a node names for `sorry` and for an axiom other than `propext`, `Classical.choice` and `Quot.sound`. |
| Q3 | It reads the replay record (`records/replay.json`, or the file of `-replay`). `yes` needs a checker other than the Lean kernel. With no record, or with `-quick`, the row says that the replay was not run. |
| Q4 | With `-lean`, it checks `manifests/statements.sha256`. The comparator result of the replay record gives `yes` or `no`. |
| Q5 | It counts the statements that pass the fidelity checks of their class. |
| Q6 | It lists the hypotheses of the proved-mod nodes and the expert status of their papers. |
| Q7, Q8 | A person answers them, so they are `not run`. |
| Q9 | It reads `records/audit.json`. `yes` needs the steps A1 to A8 done, no open finding of severity 1 or 2, and `passed`. With no record, the row is `not run`. |
| Q10 | A person answers it from the letters in the claim records, so it is `not run`. |

The replay record is JSON: `{"commit": "...", "date": "YYYY-MM-DD", "run": "<link>", "checkers": [{"name": "lean4lean", "independent": true, "ok": true}], "comparator": {"ok": true}}`. The trust workflow of mmp-lean writes it, and the comparator part is optional.

## The audit record

`mmp audit` keeps `records/audit.json`, the state of the audit of AxiomMath/MMP in the steps A1 to A8 of spec 04.3. An exit record of a step is a file of `audit/<name>/` whose name starts with the step, for example `audit/axiom-mmp/A1-build.md`. `mmp audit step` does not set a step to `done` when the step has no exit record. `mmp audit finding -severity N add` and `close` count the open findings of each severity of spec 14.5. No command sets `passed` to true. A person sets it when all the steps are done and no finding of severity 1 or 2 is open, and the commands set it to false again when this is not true.

`mmp audit axioms` is step A2. It writes a Lean file that imports `MMP` and has one `#print axioms` line for each declaration, runs it with `lake env lean` in the built checkout of `-source`, and compares each list with `propext`, `Classical.choice` and `Quot.sound`. A declaration that Lean does not find, or that uses another axiom, makes the command exit with 1. The flag `-log` keeps the full output of Lean, and `-json` writes the report.

The site shows the steps, the exit records and the open findings on `/audit/`, and writes the record to `/api/audit.json`.

## The paper records

`mmp paper add` writes a new `papers/<key>.yaml` in the schema of spec 07.13. It does not fetch the metadata from arXiv or from the DOI yet, so the flags give the key, the corpus id, the title, the tier, the reason and the expert status. An arXiv id or a DOI after the flags goes into `arxiv` or `doi`, and the version of an arXiv id becomes the version that was read. A field with no value is `null`. A reading with the tag P or S needs `-read-by` and `-read-date`. The command runs the checks of `mmp check` on the new record and writes nothing if they fail. It never overwrites a file. `mmp paper show` prints the status of each result from the map, and it reports a status in the record that is old.

## The lab

`mmp lab run` runs a spec of `lab/specs/` and writes `lab/runs/<id>/` with `record.json`, `summary.md` and the outputs. The id is the date, the name of the spec and the first 8 characters of the sha256 of the spec. The record has the hash of each file, the commit of mmp and the wall time, so a run is reproducible. The command does not replace a run unless you give `-force`, and it exits with code 1 when an output contradicts a prediction.

Only E2 has code yet. It lists the isolated cyclic quotient germs 1/r(a_1, ..., a_n) of dimension 3 or 4 up to a maximum index, and computes the mld at the origin of each germ from the ages of the points of the box (the criterion of Reid and Tai). In dimension 3 it checks two known results: the terminal lemma of Morrison and Stevens (a germ is terminal if and only if two weights add to 0 mod r), and the mld 1 + 1/r of a terminal germ. In dimension 4 it records the counts and the mld values for Target T, with no prediction. The run of dimension 3 up to index 200 takes about 10 seconds and finds no mismatch in 199,746 germs.

What the run does not do:

- It runs no Magma, Macaulay2 or other tool of spec 13.1, and it uses no container with pinned images.
- It covers only the isolated cyclic quotients, not every simplicial toric germ.
- It is a search, not a proof. It writes no certificate for Lean yet (spec 13.4).
