export const meta = {
  name: 'issue-pipeline',
  description: 'Run one GitHub issue end-to-end: triage → (PM → Architect for big work) → Dev → Reviewer ∥ Writer → fix loop → gated autonomous merge',
  whenToUse: 'Invoke as /issue-pipeline <issue-number> to process a single issue through the full agent pipeline. One issue at a time — never run two of these concurrently (dev stages share CHANGELOG.md and must stay serialized).',
  phases: [
    { title: 'Triage', detail: 'classify: full pipeline vs collapsed flow' },
    { title: 'Spec & Design', detail: 'PM spec + architect design (full tier only)' },
    { title: 'Implement', detail: 'dev agent: branch, code, tests, CHANGELOG, PR' },
    { title: 'Review', detail: 'reviewer ∥ writer, fix loop until both approve' },
    { title: 'Merge', detail: 'safety-gated autonomous merge + cleanup' },
  ],
}

const REPO = 'vibewarden/vibewarden'
const issue = String(args ?? '').match(/\d+/)?.[0]
if (!issue) throw new Error('Usage: /issue-pipeline <issue-number>')

// ---------- schemas ----------
const TRIAGE = {
  type: 'object', additionalProperties: false,
  required: ['tier', 'title', 'reason'],
  properties: {
    tier: { type: 'string', enum: ['full', 'light'], description: 'full = feature/epic/ADR-worthy or touches architecture; light = bug fix, docs, chore, drift fix' },
    title: { type: 'string' },
    reason: { type: 'string' },
    already_done: { type: 'boolean', description: 'true if the issue appears already resolved on main' },
  },
}
const DEV_OUT = {
  type: 'object', additionalProperties: false,
  required: ['pr_number', 'branch', 'summary'],
  properties: {
    pr_number: { type: 'integer' },
    branch: { type: 'string' },
    summary: { type: 'string' },
  },
}
const VERDICT = {
  type: 'object', additionalProperties: false,
  required: ['verdict', 'summary'],
  properties: {
    verdict: { type: 'string', enum: ['approve', 'changes'] },
    summary: { type: 'string' },
    blocking_items: { type: 'array', items: { type: 'string' } },
  },
}
const POSTED = {
  type: 'object', additionalProperties: false,
  required: ['reviewer', 'writer'],
  properties: {
    reviewer: { type: 'string', enum: ['approve', 'changes', 'missing'], description: 'verdict of the newest "Reviewer Agent:" comment on the PR' },
    writer: { type: 'string', enum: ['approve', 'changes', 'missing'], description: 'verdict of the newest "Writer Agent:" comment on the PR' },
    detail: { type: 'string', description: 'the two first lines, verbatim' },
  },
}
const MERGE_OUT = {
  type: 'object', additionalProperties: false,
  required: ['merged'],
  properties: {
    merged: { type: 'boolean' },
    escalate_reason: { type: 'string', description: 'set when merged=false: why a human must look' },
  },
}

// ---------- Triage ----------
phase('Triage')
const triage = await agent(
  `Read GitHub issue #${issue}: gh issue view ${issue} --repo ${REPO} --comments. ` +
  `Also skim recent commits (git log --oneline -15) to check it is not already fixed. ` +
  `Classify the work: tier "full" if it is a feature, epic, security-sensitive change, or anything ` +
  `ADR-worthy per CLAUDE.md (new domain concept, new port, wire-format change, new CLI verb); ` +
  `tier "light" for bug fixes, docs/drift fixes, chores, and dependency bumps.`,
  { label: `triage:#${issue}`, effort: 'low', schema: TRIAGE },
)
if (!triage) throw new Error('triage agent failed')
if (triage.already_done) return { skipped: true, reason: `#${issue} appears already resolved: ${triage.reason}` }
log(`#${issue} "${triage.title}" → ${triage.tier} tier (${triage.reason})`)

// ---------- Spec & Design (full tier only) ----------
let designNote = ''
if (triage.tier === 'full') {
  phase('Spec & Design')
  await agent(
    `Process GitHub issue #${issue} in ${REPO}. Turn it into a full spec with acceptance criteria ` +
    `per your standard workflow (post the spec as an issue comment, set status:ready-for-arch, lowercase labels only). ` +
    `Challenge the story if it is redundant or wrong-direction — if so, say so in the comment and stop.`,
    { agentType: 'pm', label: `pm:#${issue}`, phase: 'Spec & Design' },
  )
  const design = await agent(
    `Produce the technical design for GitHub issue #${issue} in ${REPO} per your standard workflow ` +
    `(read the PM spec from the issue comments, validate against locked decisions and the ADR index at decisions/README.md, ` +
    `post the design as an issue comment, write an ADR only if the change meets the ADR threshold, set status:ready-for-dev). ` +
    `Return a compact summary of the design: files to touch, new types/ports, acceptance criteria, non-obvious traps.`,
    { agentType: 'architect', label: `architect:#${issue}`, phase: 'Spec & Design' },
  )
  designNote = design ? `\n\nArchitect design summary:\n${design}` : ''
}

// ---------- Implement ----------
phase('Implement')
const devPrompt = (extra) =>
  `Implement GitHub issue #${issue} in ${REPO} per your standard workflow. ` +
  `${triage.tier === 'full' ? 'The architect design is in the issue comments.' : 'This is a light-tier change: plan briefly from the issue itself (no PM/architect stage ran), keep the diff minimal.'} ` +
  `Hard requirements: branch from origin/main; /usr/bin/make check must pass before the PR; ` +
  `CHANGELOG.md entry is mandatory; never leave a docs-only commit as the PR head; add files by path, never git add -A. ` +
  `${extra}${designNote} Return the PR number and branch name.`

let dev = await agent(devPrompt('Open the PR when done.'), {
  agentType: 'dev', label: `dev:#${issue}`, schema: DEV_OUT,
})
if (!dev?.pr_number) throw new Error('dev stage did not produce a PR')
log(`PR #${dev.pr_number} opened on ${dev.branch}`)

// ---------- Review (fix loop) ----------
phase('Review')
const reviewOnce = async (round) => parallel([
  () => agent(
    `Review PR #${dev.pr_number} in ${REPO} (linked issue #${issue}) per your standard two-pass workflow ` +
    `(coverage pass, then verify pass; inline comments only for verified must-fix findings; ` +
    `post "Reviewer Agent: APPROVED" or "Reviewer Agent: CHANGES REQUESTED" as the FIRST LINE of a PR comment). ` +
    `Compose the comment body inline (--body "$(cat <<'EOF' ... EOF)"); never --body-file from a fixed scratchpad path, ` +
    `which posts another agent's verdict when noclobber blocks the write (#1504). ` +
    `${round > 1 ? `This is re-review round ${round}: verify the previous blocking items were fixed, resolve addressed threads, do not re-litigate what you already approved.` : ''}`,
    { agentType: 'reviewer', label: `reviewer:r${round}:#${dev.pr_number}`, phase: 'Review', schema: VERDICT },
  ),
  () => agent(
    `Act as the documentation reviewer for PR #${dev.pr_number} in ${REPO} (linked issue #${issue}). ` +
    `Check every doc surface affected by the diff (README, docs/, reference configs, CLI help, llms files) for drift ` +
    `against the actual code changes — code in internal/ is canonical. ` +
    `Post "Writer Agent: APPROVED" or "Writer Agent: CHANGES REQUESTED" as the FIRST LINE of a PR comment with specifics. ` +
    `Compose the comment body inline (--body "$(cat <<'EOF' ... EOF)"); never --body-file from a fixed scratchpad path, ` +
    `which posts another agent's verdict when noclobber blocks the write (#1504). ` +
    `${round > 1 ? `This is re-review round ${round}: check only whether your previous blocking items were addressed.` : ''}`,
    { agentType: 'writer', label: `writer:r${round}:#${dev.pr_number}`, phase: 'Review', schema: VERDICT },
  ),
])

// The structured verdict is authoritative; the posted comment is what the merge gate
// and any human reads. They diverge when an agent posts a stale --body-file (#1504),
// so stop the round rather than run a fix loop on someone else's blocking items.
const readPostedVerdicts = (round) => agent(
  `Report what is currently POSTED on PR #${dev.pr_number} in ${REPO} — do not review the PR yourself. ` +
  `Read both surfaces, one line per comment (created_at TAB first line of the body): ` +
  `gh api repos/${REPO}/issues/${dev.pr_number}/comments --jq '.[] | "\\(.created_at)\\t\\(.body | split("\\n")[0])"' | cat, ` +
  `and gh api repos/${REPO}/pulls/${dev.pr_number}/reviews --jq '.[] | "\\(.created_at)\\t\\(.body | split("\\n")[0])"' | cat. ` +
  `Across both surfaces take the line with the latest created_at containing "Reviewer Agent:" and the latest containing "Writer Agent:". ` +
  `Map APPROVED -> approve, CHANGES REQUESTED -> changes, and use "missing" when no such comment exists.`,
  { label: `posted-check:r${round}:#${dev.pr_number}`, effort: 'low', schema: POSTED },
)

let approved = false
for (let round = 1; round <= 3; round++) {
  const [rev, wri] = await reviewOnce(round)
  if (!rev || !wri) throw new Error(`review round ${round}: an agent failed`)
  const posted = await readPostedVerdicts(round)
  const drift = [
    posted?.reviewer !== rev.verdict ? `reviewer returned "${rev.verdict}" but posted "${posted?.reviewer ?? 'unreadable'}"` : null,
    posted?.writer !== wri.verdict ? `writer returned "${wri.verdict}" but posted "${posted?.writer ?? 'unreadable'}"` : null,
  ].filter(Boolean)
  if (drift.length) {
    log(`round ${round}: POSTED COMMENT MISMATCH — ${drift.join('; ')}`)
    return {
      issue: Number(issue), tier: triage.tier, pr: dev.pr_number, merged: false,
      escalate: `review round ${round}: posted comments disagree with the returned verdicts (${drift.join('; ')}). ` +
        `Likely a stale --body-file post (#1504) — fix the comments on the PR by hand before re-running.`,
    }
  }
  if (rev.verdict === 'approve' && wri.verdict === 'approve') { approved = true; break }
  const blockers = [...(rev.blocking_items ?? []), ...(wri.blocking_items ?? [])]
  log(`round ${round}: changes requested (${blockers.length} items) — dispatching fix`)
  if (round === 3) break
  await agent(
    devPrompt(
      `The PR already exists (#${dev.pr_number}, branch ${dev.branch}) — do NOT create a new branch or PR. ` +
      `Check out ${dev.branch}, address exactly these review items, run /usr/bin/make check, push. Items:\n- ${blockers.join('\n- ')}`,
    ),
    { agentType: 'dev', label: `dev-fix:r${round}:#${dev.pr_number}`, phase: 'Review', schema: DEV_OUT },
  )
}
if (!approved) return { merged: false, pr: dev.pr_number, escalate: 'not approved after 3 review rounds — human review needed' }

// ---------- Merge (safety-gated) ----------
phase('Merge')
const merge = await agent(
  `You are the merge gate for PR #${dev.pr_number} in ${REPO}. Both the reviewer and writer agents have approved. ` +
  `Steps, in order — abort with merged=false and an escalate_reason if ANY gate fails:\n` +
  `1. SANITY GATE: gh pr diff ${dev.pr_number} --repo ${REPO} --stat. If more than 50 files changed, or deletions look like mass file removal (hundreds of files), DO NOT MERGE — escalate.\n` +
  `2. Confirm both approval comments exist on the PR ("Reviewer Agent: APPROVED" and "Writer Agent: APPROVED").\n` +
  `3. Checks: gh pr checks ${dev.pr_number} --repo ${REPO}. Wait and poll (up to ~10 min) while required Go checks run. ` +
  `If checks list is empty because CI workflows are disabled, proceed (make check was enforced locally). ` +
  `If mergeStateStatus is BLOCKED with required checks absent on a code PR, that is the docs-only-head path-filter trap — escalate, do not poll forever.\n` +
  `4. Resolve remaining review threads via the GraphQL resolveReviewThread mutation (they were addressed by the fix rounds).\n` +
  `5. Merge: gh pr merge ${dev.pr_number} --repo ${REPO} --squash --delete-branch. NEVER use --admin under any circumstances.\n` +
  `6. Remove all status:* labels from the PR and issue #${issue}; verify the issue auto-closed, close it manually if not.\n` +
  `7. Run: "$CLAUDE_PROJECT_DIR"/.claude/hooks/post-merge-cleanup.sh ${dev.branch} (falls back to .claude/hooks/post-merge-cleanup.sh from the repo root).`,
  { label: `merge:#${dev.pr_number}`, effort: 'medium', schema: MERGE_OUT },
)
return {
  issue: Number(issue),
  tier: triage.tier,
  pr: dev.pr_number,
  merged: merge?.merged ?? false,
  escalate: merge?.escalate_reason ?? null,
}
