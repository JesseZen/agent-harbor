# Issue tracker: GitHub

Issues and specifications live in **JesseZen/agent-harbor** on GitHub. Use the `gh` CLI and explicitly target this repository. The retired private tracker is historical context, not a source of current blocking edges.

## Operations

- Before writing, verify the target with `gh repo view JesseZen/agent-harbor --json nameWithOwner,url`.
- Read the complete issue body and comments with `gh issue view NUMBER --repo JesseZen/agent-harbor --json number,title,body,labels,comments,state`.
- List issues with `gh issue list --repo JesseZen/agent-harbor --state open --json number,title,labels,assignees` and relevant filters.
- Create one issue per approved ticket with `gh issue create --repo JesseZen/agent-harbor --title TITLE --body-file FILE`. Use an exact temporary UTF-8 file for multiline bodies and comments.
- Use native sub-issues for children of an existing source issue. Where supported, use the installed CLI's sub-issue operations; otherwise POST to `repos/JesseZen/agent-harbor/issues/PARENT/sub_issues` with the child's numeric database `id` as `sub_issue_id`. A body reference is the fallback when native relationships are unavailable.
- Represent blockers with native dependencies. Where supported, use the installed CLI's dependency operations; otherwise POST to `repos/JesseZen/agent-harbor/issues/CHILD/dependencies/blocked_by` with the blocker's numeric database `id` as `issue_id`. Issue numbers and GraphQL node IDs are not database IDs. When native edges are unavailable, put explicit linked **Blocked by** entries in the issue body and record the fallback.
- A ticket can start when every blocker is completed. A scheduling preference is not a blocking edge.
- Use the label mapping in `triage-labels.md`. `ready-for-agent` means sufficiently specified; it does not override dependencies.
- Publish evidence with `gh issue comment NUMBER --repo JesseZen/agent-harbor --body-file FILE`. Close a ticket only when its acceptance criteria are met and its implementation is integrated. Keep unmet criteria explicit. Do not close or modify a parent issue as an incidental effect of ticket decomposition.

## Publication boundary

GitHub Issues are public. Keep machine paths, private prompts, credentials, real account identifiers, conversation content and unsanitized runtime evidence local. Issue bodies must contain enough sanitized decisions and acceptance criteria to stand alone; a private file link is not a substitute.

An instruction to publish tickets authorizes those issue writes. Pushes, PRs, releases and upstream messages require their own authorization.

## Pull requests as a triage surface

**PRs as a request surface: no.**

## Skill meanings

“Publish to the issue tracker” means create a GitHub issue in this repository. “Fetch the relevant ticket” means read its full body, comments and blocking relationships.
