export const meta = {
  name: 'gvim-followup-wave',
  description: 'Follow-up wave: parallel worktree tracks fixing dogfood findings and UX requests, integrator, per-track review, skeptics, fixers, closure audit, final gates',
  phases: [
    { title: 'Implement', detail: 'one agent per track in its own worktree' },
    { title: 'Integrate', detail: 'apply patches to the main tree, run gates' },
    { title: 'Review', detail: 'read-only reviewer per track, skeptic per finding' },
    { title: 'Fix', detail: 'sequential fixers' },
    { title: 'Closure', detail: 'verify every assigned item is really closed; final gates' },
  ],
}

const ROOT = '/Users/tbxark/Repos/tbxark/gvim'
const FRESH = ROOT + '/refs/fresh'
const FU = '/tmp/gvim-followup'
const BASE = args.base
const PATCHES = `/tmp/gvim-patches/${args.wave}`
const TRACKS = args.tracks

const GATES = `Gates (run all before finishing): go build ./... && CGO_ENABLED=0 go build ./... ; go vet ./... ; golangci-lint run ./... ; bash migration/tools/purity_check.sh ; bash migration/tools/import_rules_check.sh ; go mod tidy -diff ; go test ./... ; go test -race on the packages you touched (cgo permitting). All must pass. Never edit generated files (internal/palette/commands_gen.go, internal/ui/menubar/menu_table_gen.go: if the menu table must change, change its generator or add a hand-written layer, per CLAUDE.md; python3 migration/tools/gen_menus.py --check reports drift) and never touch refs/ or _attic/. Project test rules (CLAUDE.md): plain table-driven Go unit tests; ui code is tested by driving Update with messages and asserting model state, tea.Cmds, layout rectangles and hit-map results, or a few specific grid cells; NO golden/snapshot/full-frame tests, no testdata screen dumps, no new migration/parity scenarios; do not regenerate existing fixtures (a test that pins behaviour the user asked to change may be rewritten, say so). Fix each bug with a regression test that fails first. Never delete or weaken a test just to pass. Never run git commit/push/stash/reset/checkout/clean. Leave no scratch files in the repo.`

const CTX = `HARD CONSTRAINT FROM THE USER: never fork, patch, vendor, or add go.mod replace directives for third-party modules (especially github.com/yuin/gopher-lua): upstream dependencies must stay untouched so the project stays maintainable. Solve problems inside gvim's own code only (configuration through the library's exported API, wrappers, our own compile step), or document the limitation. Project: gvim (${ROOT}), a Go terminal editor (Bubble Tea v2), port of the Fresh TUI (Rust reference, READ-ONLY at ${FRESH}; it is git-ignored so it is not inside worktrees: use that absolute path). Read ${ROOT}/CLAUDE.md and migration/ARCH-v4.md (import graph; ui/layout.go is the single geometry source for painting and the hit map; ui sub-packages must not import ui; Update does no IO; domain packages must not import bubbletea/lipgloss). Context: nine simulated users exercised the real binary; their reports are in ${FU}: papercuts.json (54 UX papercuts, each with pid), confirmed.json (85 bugs found, most already fixed), fix-reports.md (what the fixers did and what they left partial or undecided). The user's instructions, and the resolved decisions for nine open points, are in ${FU}/DECISIONS.md: READ IT FIRST and follow it exactly (rule: follow Fresh's behaviour after verifying it in ${FRESH}; if Fresh has none, use best practice; say in your report which rule you followed). The real-binary pty harness is ${FU}/../gvim-dogfood/README.md and driver.py (venv at /tmp/gvim-dogfood/venv; build a private binary with go build -o /tmp/gvim-dogfood/<name>/gvim . in your worktree; isolated HOME is the default; pyte leaves stale cells after scrolls, cross-check suspicious frames with tmux if available). internal/uitest can render a model to PNG (see internal/uitest/png.go and font.go): use it to look at screenshots when you change visuals (Read the PNG file), writing them under /tmp, never into the repo.`

const REPORT = {
  type: 'object',
  properties: {
    track: { type: 'string' },
    patch: { type: 'string' },
    summary: { type: 'string' },
    rules_followed: { type: 'array', items: { type: 'string' }, description: 'for each decision/behaviour: what Fresh does (file ref) or which best-practice you applied' },
    closed_items: { type: 'array', items: { type: 'string' }, description: 'papercut pids and confirmed-finding ids fully closed, each with a test' },
    partial_items: { type: 'array', items: { type: 'object', properties: { id: { type: 'string' }, what_is_missing: { type: 'string' } }, required: ['id', 'what_is_missing'] } },
    not_done: { type: 'array', items: { type: 'object', properties: { id: { type: 'string' }, reason: { type: 'string' } }, required: ['id', 'reason'] } },
    existing_tests_changed: { type: 'array', items: { type: 'string' } },
    gates_passed: { type: 'boolean' },
  },
  required: ['track', 'patch', 'summary', 'closed_items', 'partial_items', 'not_done', 'gates_passed'],
}

phase('Implement')
const runs = await parallel(TRACKS.map(t => () => agent(
`WORKSPACE SETUP (first): you are in a fresh git worktree of ${ROOT}. Run: git reset --hard ${BASE} . That dangling snapshot commit is the current project state (it includes uncommitted work of the main tree). Other tracks run in parallel in their own worktrees and an integrator merges the patches with git apply --3way, so: keep to your track's scope, put new code in new files where natural, keep edits to hot shared files (internal/ui/model.go, view.go, layout.go, ports.go, internal/editor/handlers.go, document.go) as small hunks, do not reformat or reorder existing code.

${CTX}

YOUR TRACK: ${t.id} — ${t.title}

Assigned items: papercut pids ${JSON.stringify(t.pids)} from ${FU}/papercuts.json; other items named in the brief below. Read each assigned record fully, then verify the claim against the current code before building on it (the code changed since the report; some items may already be fixed: then just add a test and report closed). If an item clearly belongs to your area but is not assigned to any track you can see, take it and say so.

BRIEF:
${t.brief}

Method: for each item write a failing test first (domain test, ui Update-driven test, or plugin test; for visual/layout work assert on rectangles, hit-map results and specific cells, and ALSO look at PNG screenshots before and after), then implement with minimal surgical changes that match the surrounding style. "Fix all" is the instruction: do not skip an item because it is hard; if something is truly impossible, say exactly why in not_done. Report honestly (partial_items for anything incomplete).

${GATES}

EXPORT (last step): in your worktree root run: git add -A && mkdir -p ${PATCHES} && git diff --cached ${BASE} --binary > ${PATCHES}/${t.id}.patch ; report that path.`,
  { label: `track:${t.id}`, phase: 'Implement', isolation: 'worktree', schema: REPORT })))

const ok = runs.map((r, i) => r ? { ...r, track: TRACKS[i].id, title: TRACKS[i].title } : null).filter(Boolean)
const failed = TRACKS.filter((t, i) => !runs[i]).map(t => t.id)
if (failed.length) log('tracks with no result: ' + failed.join(', '))

phase('Integrate')
const integ = await agent(
`You are the INTEGRATOR in the main tree ${ROOT} (sole writer; never commit, stash, reset, checkout or clean). The patches below were made relative to snapshot ${BASE} (commit 2ab7364). The main tree has moved since then: it equals rollback snapshot 1b1dcc4b79b3d5a0539dcc36abb6b9e103fb9a82 (dangling, fork removed on purpose). The diff 2ab7364..1b1dcc4 is the user's post-snapshot work and MUST be preserved: check it with git diff --stat 2ab7364 1b1dcc4 before starting. Patches, in this order:\n${ok.map(r => `- ${r.track} (${r.title}): ${r.patch}`).join('\n')}\nApply each with git apply --3way (the index already matches 1b1dcc4; if it complains, git add -A -- . ':!.claude' first). After EACH patch run go build ./... and go vet ./... and resolve conflicts preserving both sides' intent. HARD RULES: (1) never add a go.mod replace directive or a vendored third_party/ copy of gopher-lua or any other dependency; if a patch reintroduces them, drop that hunk and report it. (2) The gopher-lua fork has been removed on purpose (the user does not want upstream dependencies modified). Plugin tests that depended on it must be rewritten to assert the fork-free behaviour: internal/plugin/concat_cap_test.go (delete it, or replace its assertions with a bounded-residual measurement), the 'string length overflow' expectations in budgets_test.go and io_fs_test.go (expect the heap guard's memory-limit message or the stock behaviour instead), the callTypeError wording in errfmt_test.go (expect the stock gopher-lua wording: attempt to call 'x', which is not a function), and any stdlib_test.go cap assertion that needs the fork. Report every rewrite with its reason. Never weaken a test beyond that. (3) Do not edit generated files. Then run the gates: go build ./... && CGO_ENABLED=0 go build ./... ; go vet ./... ; golangci-lint run ./... ; bash migration/tools/purity_check.sh ; bash migration/tools/import_rules_check.sh ; go mod tidy -diff ; go test ./... ; go test -race on internal/ui/... internal/editor internal/buffer internal/search internal/keymap internal/plugin/... internal/app internal/config (cgo permitting). Redirect output to files and read exit codes. Fix failures at the root cause. Track reports:\n${JSON.stringify(ok.map(r => ({ track: r.track, summary: r.summary.slice(0, 700), rules: r.rules_followed, partial: r.partial_items, not_done: r.not_done, tests_changed: r.existing_tests_changed })), null, 1)}\n\nReturn per-patch status, conflicts and resolutions, the test rewrites, anything dropped or altered, exact gate results, unresolved failures.`,
  { label: 'integrator-2', phase: 'Integrate' })

phase('Review')
const FINDINGS = {
  type: 'object',
  properties: { findings: { type: 'array', items: { type: 'object', properties: {
    id: { type: 'string' }, severity: { type: 'string', enum: ['high', 'medium', 'low'] },
    kind: { type: 'string', enum: ['bug', 'not-closed', 'regression', 'ux', 'missing-test', 'integration'] },
    where: { type: 'string' }, problem: { type: 'string' }, repro: { type: 'string' }, fix: { type: 'string' } },
    required: ['id', 'severity', 'kind', 'where', 'problem', 'repro', 'fix'] } } },
  required: ['findings'],
}
const VERDICT = { type: 'object', properties: { real: { type: 'boolean' }, reasoning: { type: 'string' }, evidence: { type: 'string' } }, required: ['real', 'reasoning', 'evidence'] }

const reviewed = await pipeline(
  ok,
  r => agent(
`Independent review of track ${r.track} (${r.title}), merged into the main tree ${ROOT} (gates pass). READ-ONLY: edit nothing in the repo (temporary scratch tests must be deleted; scratch programs/PNGs under /tmp). ${CTX}\n\nThe implementer reports (may be optimistic): ${JSON.stringify({ summary: r.summary.slice(0, 1200), closed: r.closed_items, partial: r.partial_items, not_done: r.not_done, rules: r.rules_followed })}\nThe track brief:\n${TRACKS.find(t => t.id === r.track).brief}\n\nFor EVERY closed item: locate the implementation and its test, run the test, then try to break it (narrow/short terminals 60x20 and 80x24 and wide 200x50, CJK text, multi-cursor, wrapped lines, large-file mode, read-only buffers, each keymap preset, mouse and keyboard, resize during the interaction). For the visible-UI tracks build a binary and use the pty harness plus PNG screenshots from internal/uitest to look at the real result (is it actually less cramped? does hover behave? does every shown shortcut work?). Check that the stated Fresh rules were really followed (compare with ${FRESH}). Report only concrete reproducible problems (with a failing test sketch where possible) and any item reported closed that is not. Empty list is valid.`,
    { label: `review:${r.track}`, phase: 'Review', schema: FINDINGS }),
  (res, r) => parallel((res?.findings ?? []).map(f => () => agent(
`You are a skeptic for gvim (${ROOT}, main tree, read-only for you; delete any scratch test you add). A reviewer claims about track ${r.track}: ${JSON.stringify(f, null, 1)}\nTry to REFUTE it (reproduce with a scratch test or the pty harness, read the code, compare with Fresh at ${FRESH}, check ${FU}/DECISIONS.md: if the behaviour is exactly what the user asked for it is not a problem). real=true only if reproduced or proven by reading the code.`,
    { label: `skeptic:${r.track}:${f.id}`, phase: 'Review', schema: VERDICT }).then(v => ({ track: r.track, ...f, verdict: v })))),
)
const confirmed = reviewed.filter(Boolean).flat().filter(f => f && f.verdict && f.verdict.real)
log(`${confirmed.length} confirmed review findings`)

phase('Fix')
const fixReports = []
for (const r of ok) {
  const mine = confirmed.filter(f => f.track === r.track)
  const open = [...(r.partial_items ?? []), ...(r.not_done ?? [])]
  if (!mine.length && !open.length) continue
  const fx = await agent(
`You are the FIXER for track ${r.track} (${r.title}) in the main tree ${ROOT} (sole writer; never commit/stash/reset/checkout/clean). ${CTX}\n\n1. Confirmed review findings (independently reproduced); failing regression test first, then root-cause fix:\n${JSON.stringify(mine.map(f => ({ id: f.id, severity: f.severity, kind: f.kind, where: f.where, problem: f.problem, repro: f.repro, fix: f.fix })), null, 1)}\n\n2. Items the implementer left partial or not done (the user said FIX ALL: finish them; only if truly impossible say exactly why):\n${JSON.stringify(open, null, 1)}\n\nTrack brief for context:\n${TRACKS.find(t => t.id === r.track).brief}\n\n${GATES}\n\nReturn fixed items with test names, anything still open and why.`,
    { label: `fix:${r.track}`, phase: 'Fix' })
  fixReports.push({ track: r.track, report: fx })
}

phase('Closure')
const closure = await agent(
`Closure audit for the follow-up wave "${args.wave}" in ${ROOT} (read-only except where noted). Tracks in this wave and their assigned papercut pids:\n${JSON.stringify(TRACKS.map(t => ({ id: t.id, pids: t.pids })))}\nFor EVERY assigned pid in ${FU}/papercuts.json (read each record): decide closed / partly / open by reading the current code and tests and, where cheap, exercising the behaviour (pty harness, a scratch Update-driven test that you delete). Also check the user's binding requests listed in ${FU}/DECISIONS.md that belong to this wave's tracks. Return the table. Do not modify files.`,
  { label: 'closure', phase: 'Closure',
    schema: { type: 'object', properties: { items: { type: 'array', items: { type: 'object', properties: { id: { type: 'string' }, status: { type: 'string', enum: ['closed', 'partly', 'open'] }, evidence: { type: 'string' }, remaining: { type: 'string' } }, required: ['id', 'status', 'evidence'] } } }, required: ['items'] } })
const notClosed = (closure?.items ?? []).filter(i => i.status !== 'closed')
let closeFix = null
if (notClosed.length) {
  closeFix = await agent(
`You are the final FIXER in the main tree ${ROOT} (sole writer; never commit/stash/reset/checkout/clean). ${CTX}\nThese assigned items were found only partly closed or open by an independent audit; finish them (failing test first, root-cause fix, surgical changes). The user said FIX ALL; if something is genuinely impossible, say exactly why.\n${JSON.stringify(notClosed, null, 1)}\n\n${GATES}\n\nReturn what you fixed (with tests) and what remains.`,
    { label: 'fix:closure', phase: 'Closure' })
}
const final = await agent(
`Final gate run in ${ROOT} (sole writer): go build ./... ; CGO_ENABLED=0 go build ./... ; go vet ./... ; golangci-lint run ./... ; bash migration/tools/purity_check.sh ; bash migration/tools/import_rules_check.sh ; gofmt -l . (excluding refs and _attic) ; go mod tidy -diff ; python3 migration/tools/gen_menus.py --check (generated menu table must not drift) ; go test ./... ; go test -race on internal/plugin/... internal/pluginapi/... internal/luapat internal/decor internal/ui/... internal/editor internal/buffer internal/search internal/keymap internal/app internal/config . Redirect outputs to files and report exact exit codes. Fix anything red at the root cause (never weaken tests; make time-budget tests deterministic). Remove stray scratch files in the repo and big files in /tmp that you or earlier agents created (/tmp/gvim-dogfood/*/gvim binaries are fine to remove). Do not commit.`,
  { label: 'final-gates', phase: 'Closure' })

return {
  tracks: ok.map(r => ({ track: r.track, summary: r.summary, rules: r.rules_followed, closed: r.closed_items, partial: r.partial_items, not_done: r.not_done, tests_changed: r.existing_tests_changed })),
  failed, integ,
  confirmed: confirmed.map(f => ({ track: f.track, id: f.id, severity: f.severity, where: f.where, problem: f.problem })),
  fixReports, closure: closure?.items ?? null, closeFix, final,
}
