# Reviewing and Revising Plans in the Panel

This guide covers the Artifact Review and Plan Mutation workflow in the
Punakawan panel: adding anchored comments to a plan, submitting a review,
reading Punakawan's proposed revision, and accepting, rejecting, or
requesting further changes. It ends with a troubleshooting section for the
errors you're most likely to hit.

The full design is in
[`punakawan-artifact-review-plan-mutation-plan-v2.md`](../../punakawan-artifact-review-plan-mutation-plan-v2.md)
at the repo root. This document is the user-facing "how do I actually do
this" companion to that plan.

The panel never edits a plan directly. Every change goes through a review →
proposal → acceptance cycle, and only an explicitly accepted proposal
becomes the plan's next canonical version. Earlier versions are never
overwritten.

## Starting a review

1. Open **Start Review** and fill in the plan's ID (e.g. `plan-panel`), a
   review title, and an optional general instruction.
2. Choose **Start Review**. This creates a new draft review against the
   plan's current version and takes you straight to **Review Mode** for it.
3. Review Mode shows the plan's current version and revision hash in the
   page header, and the plan's rendered content below it. The review
   starts in **draft** status: nothing you do in this state is visible to
   anyone else and nothing has been submitted to Punakawan yet — you can
   add, edit, and delete comments freely, and close the tab and come back
   later without losing anything (drafts are saved as you go).

If you navigate away mid-review and come back, the panel refetches
everything from the server and picks up exactly where you left off,
including any draft comments and your review instruction.

## Adding comments

There are two ways to anchor a comment to the document:

- **Section comment**: hover (or, on a touch device, the affordance is
  always visible) over a section heading and choose **+ Comment on
  section**. The comment anchors to that section as a whole.
- **Selected-text comment**: select a run of text in the document body. A
  small **Comment on selection** popover appears near the selection —
  choose it to anchor the comment to that exact quoted text.

Either way, a comment composer opens showing the heading path and (if
applicable) the quoted text the comment will anchor to. Type your comment
and choose **Add Comment**, or **Cancel** to discard it.

On desktop, comments appear in a rail alongside the document. On mobile,
comments live in a bottom sheet — tap **View Comments** to open it.

### Editing and resolving comments

While the review is still a draft, every comment you've added can be
edited or deleted from the comment rail (or bottom sheet on mobile):

- **Edit** changes the comment body in place.
- **Delete** marks the comment obsolete — it's excluded from what gets
  submitted, but the record isn't destroyed (it still shows, dimmed, if you
  choose to see obsolete comments).

Each comment shows a status chip. While drafting, comments stay **Open**.
Later statuses (**Addressed**, **Partially addressed**, **Needs
clarification**, **Rejected by agent**, **Resolved**) only appear once
Punakawan has processed the review — see "Reviewing a proposal" below.

### General review instruction

Above the document, the **Review Instruction** panel accepts free-text
guidance that isn't anchored to any specific section — for example, "Apply
all comments while preserving unaffected detail." This autosaves shortly
after you stop typing; a small status indicator shows **Saved**,
**Saving…**, or **Unsaved changes**.

## Submitting for revision

When your comments are ready, choose **Submit Review** (a sticky action at
the bottom of the page, so it's reachable without scrolling back up).

Submitting:

- Freezes an immutable snapshot of your comments and instruction — nothing
  after this point can silently change what Punakawan actually revises
  from.
- Dispatches exactly one revision run. If you click Submit more than once
  (or a network retry resends the request), the panel and server both treat
  it as the same submission — you will not get two competing revisions.
- Moves the review out of draft. The document and comment-editing UI are
  replaced by an **active revision** status view.

You can no longer add or edit comments once submitted. If you need to
change your mind before the revision is picked up, use **Cancel Review**
(with a confirmation dialog) — this stops the tracked run from being
followed further; it does not touch the canonical plan.

### While the revision is in progress

The active-revision view shows:

- Current status (queued, revising, awaiting clarification, and so on),
  with a short plain-language description of what that status means.
- The base version and revision hash the revision is working from.
- A comment-status breakdown chart.
- The submitted revision request's details and tracked run ID, once
  available.

The panel polls for status updates automatically every few seconds and
stops once a terminal status (accepted, rejected, cancelled, failed) is
reached — you don't need to manually refresh.

### Answering a clarification question

If Punakawan hits a comment that's materially ambiguous, the review moves
to **awaiting_clarification** and a **Needs your input** section appears
above the status summary, listing each question. Type your answer in the
box under the question and choose **Save Answer**. Punakawan resumes the
same revision attempt from where it paused — you do not lose any work
already done, and this does not create a new attempt.

## Reviewing a proposal

Once Punakawan finishes, the review moves to **proposal_ready** and the
panel replaces the active-revision view with the full **proposal review**
screen. This is where you actually inspect what changed and decide what to
do about it.

### The diff

The **Diff** section shows the proposed new version against the base
version you submitted from:

- On desktop, this renders side-by-side (base on the left, proposed on the
  right). On narrower screens, it renders as a single unified diff with
  `+`/`-` markers.
- Long unchanged runs collapse behind a **Show N unchanged lines** toggle
  so you aren't scrolling past pages of untouched content — expand any of
  them to see the full context.
- Use the search box above the diff to filter to lines containing a term;
  matches are highlighted.
- A `+N`/`-M` summary badge above the diff gives the overall size of the
  change at a glance.

### Comment resolution

The **Comment resolution** section lists every comment from your
submission, each with the resolution Punakawan reported:

- **Addressed** — the comment was acted on; the changed block IDs are
  listed underneath.
- **Partially addressed** — acted on incompletely; an explanation is
  shown.
- **Rejected** — Punakawan declined to act on it, with an explanation of
  why.
- **Not applicable** — the comment didn't require a content change.
- **Unresolved** (shown in red, bordered distinctly) — no resolution was
  reported for this comment at all. Every comment must have a resolution
  before a proposal can be accepted; an unresolved comment is a signal that
  something needs your attention before you accept.

### Validation

The **Validation** section shows two independent pass/fail reports:

- **Structural** — checks like balanced Markdown fences, unique block IDs,
  and valid heading hierarchy.
- **Review compliance** — checks like "every comment has a resolution" and
  "rejected comments include an explanation."

An **Overall** badge summarizes both. If either report fails, the
**Accept** action is disabled and a short explanation tells you to use
**Request Changes** instead.

### Version lineage

A small graph shows the base version and every attempt made so far under
this review, so you can see how many rounds of "request changes" led to
the proposal you're looking at.

## Accepting, rejecting, or requesting changes

Three actions are available at the bottom of the proposal review screen
(sticky, so they stay reachable while you scroll through the diff):

- **Accept** — creates a new canonical version of the plan from this
  proposal's content. This is the only thing in the entire workflow that
  actually changes the canonical artifact, and it requires this explicit
  action; nothing happens automatically. A confirmation dialog spells out
  that this can't be undone from here (you'd need to start a fresh review
  to make further changes). Accept is disabled while validation has
  failed.
- **Reject** — marks the review as rejected. This never touches the
  canonical plan; only the review's own status changes. Also has a
  confirmation dialog.
- **Request Changes** — dispatches another revision attempt under the same
  review, optionally with additional free-text guidance for what should
  change. The previous attempt remains available for comparison; nothing
  about the original submitted comments is altered.

## Handling a conflict (rebase)

If the plan's canonical version changes elsewhere while your review is
in flight or after a proposal is ready — for example, someone else accepted
a different review on the same plan — your proposal is no longer based on
the current canonical version. The panel marks the review **conflicted**
and shows a banner explaining that accepting is blocked until you rebase.

Choose **Rebase onto latest version** to re-point your review at the
current canonical version. This returns the review to **draft** so you can
look over the refreshed document (your original comments and instruction
carry over) and resubmit. Rebasing never silently keeps or discards
content on its own — you always see the fresh document before resubmitting.

If a conflict is only detected at the moment you choose **Accept** (the
canonical version moved in the brief window between opening the proposal
and accepting), the same rebase flow appears immediately as part of the
accept failure.

## Troubleshooting

**"Session expired"** — The panel's authenticated session has a short
lifetime and is invalidated whenever the panel process stops. Reopen the
panel from the terminal to get a fresh session; nothing about your draft
review or submitted comments is lost, since drafts persist server-side.

**Submit Review fails / doesn't respond** — Check the error message shown
below the Submit button:

- A `409` mentioning the review "is not a draft and has no matching
  pending submission" usually means the review was already submitted
  (possibly from another tab) — reload the review to see its current
  state instead of resubmitting.
- Any other failure is safe to retry — submission is idempotent, so
  retrying after a genuine network failure will not create a duplicate
  revision run.

**Validation failed on a proposal** — Accept is intentionally disabled.
Read the structural and review-compliance issue lists to see exactly what
failed, then use **Request Changes** describing what needs fixing (or
address it via new comments in a fresh review, if the base plan itself
needs to change first).

**"This proposal is out of date" / conflict banner** — See "Handling a
conflict" above: choose **Rebase onto latest version**, review the
refreshed document, and resubmit.

**A comment shows "Unresolved"** — This means Punakawan didn't report a
resolution for it at all (as opposed to rejecting it with an explanation).
Acceptance is still technically possible once validation passes, but
double-check this wasn't simply missed — consider **Request Changes** to
have it addressed explicitly, or leave a note for the next reviewer if the
comment turned out not to apply.

**Clarification question never resolves after answering** — Confirm
**Save Answer** was clicked (not just typed) and that the active-revision
view's status has moved past `awaiting_clarification`. If it's stuck,
check the tracked run ID shown in the active-revision summary against the
underlying BD task for more detail.

**The panel doesn't reflect a change made in another tab or by another
person** — Statuses update via periodic polling (every few seconds) while
a review is in flight, and via a full refetch on navigating back to the
review. If something still looks stale after a few seconds, reload the
page.
