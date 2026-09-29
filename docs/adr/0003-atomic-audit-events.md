# Audit events commit atomically with the state change, in the same transaction and database

Every status transition on a Deposit or Withdrawal writes its Audit Event in the same database transaction as the state change, and event-write failures propagate so the transaction rolls back. Previously, event insert errors were logged and swallowed inside the transaction — a withdrawal could be approved with no record of who approved it, which is unacceptable for a custody/payments audit trail.

Because events live in the same Postgres database as the state they describe, a shared transaction *is* the atomicity mechanism — no outbox table or relay is needed. An outbox becomes necessary only if events are published to an external broker (e.g. Kafka for downstream consumers); that is a documented future step, not current scope.

## Canonical event taxonomy

Event types were inconsistent (`CREATED`, `STATUS_CHANGED`, `withdrawal.approved`). All events now use `<aggregate>.<verb>`:

- `deposit.created`, `deposit.confirmation_updated`, `deposit.status_changed`
- `withdrawal.created`, `withdrawal.approved`, `withdrawal.rejected`, `withdrawal.cancelled`, `withdrawal.broadcast_claimed`, `withdrawal.confirmation_updated`, `withdrawal.status_changed`

Actor identity (Approver / Customer) rides in event `metadata.actor_id`, keeping the event schema stable while the audit trail answers "who did this."

## Consequences

- Every service mutation helper returns event-write errors instead of swallowing them; a failing audit insert now fails the operation loudly rather than silently losing the trail.
- `withdrawal.broadcast_claimed` closes the last unaudited transition (APPROVED → BROADCASTING happened inside the repository claim with no event at all).
