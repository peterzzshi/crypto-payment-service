---
name: Technical kickoff
about: Break down a feature into scope, approach, options, questions, and proposed changes before coding
title: "[kickoff] <area>: <short summary>"
labels: ["kickoff"]
---
## Summary
- What we’re solving and why.
- Link to relevant docs/designs (architecture, data model, flows).

## In Scope

## Out of Scope

## Context / Background
- Current state and constraints (providers, chains, product requirements, compliance, infra).
- Related issues/PRs.

## Approach (high-level design)
- Proposed shape (services/adapters/storage/API surface touched).
- How it fits into existing architecture and boundaries.

## Technical options & tradeoffs
1) Option A — pros/cons
2) Option B — pros/cons
3) Decision (and why), or what we need to decide.

## Proposed changes
- Code areas to touch (files/packages/services).
- Data impacts (schema/migrations, backward compatibility).
- External interfaces (internal APIs, provider payloads, product-facing fields).
- Operational/SRE considerations (env vars, alerts, limits).

## Open questions for the team
- …

## Risks / assumptions / dependencies
- …

## Testing & acceptance
- Test strategy (unit/integration/e2e), data sets, fixtures.
- Acceptance criteria / what “done” looks like.

## Comms & updates
- Who needs to review/approve.
- How we’ll share progress and summaries at each stage.

## Milestones / next steps
- Sequence of deliverables (with owners/dates if known).
