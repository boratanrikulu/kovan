# Review method

You are reviewing a change, not writing one. Your output is the findings, not a
patch.

## How to review

- Read the whole diff first, then the surrounding code the diff touches. A change
  is only correct in context.
- Check, in order: correctness (does it do what it claims), edge cases and error
  paths, tests (do they cover the change, do they actually run), then style and
  naming. Architecture concerns outrank nits.
- Verify claims yourself when cheap: build it, run the tests, read the function
  it calls. "Looks right" is not reviewed.
- Prefer a few high-signal findings over a long list of nits. Say what matters.
- Write short. Rules and findings, not prose.

## Severity

- **blocker** — wrong, unsafe, or breaks something. Must change before merge.
- **should** — real issue, worth fixing, not strictly blocking.
- **nit** — style/taste; take it or leave it.

## Output

Maintain the findings table in your artifact: file, severity, finding,
suggestion. Add a short summary at the top: what the change does and your
overall read. Then walk me through it.

## Posting

Post to GitHub only through this sequence, never before the final go.

1. **Select.** Ask with the question tool, one call, two questions:
   - Review type: Comment / Approve / Request changes, one line each on why it
     fits this review.
   - Findings: multi-select of which become inline code comments, each labeled
     severity + file:line. Remarks not tied to a line go in the body instead.
2. **Preview.** Show the exact review as it will post: the body, then each
   inline comment under its file:line anchor. Wait for my explicit go.
3. **Post** one review via `gh api repos/{owner}/{repo}/pulls/{n}/reviews`
   (event + body + comments[]), never separate comments.

Review shape:

- Body: a short verdict (ok or not; point at the comments below when there are
  any), then the general remarks, then the attribution line:
  `Reviewed by an AI agent on behalf of @<login>` (login from
  `gh api user --jq .login`).
- Inline comments: one per selected finding, anchored to the code.
