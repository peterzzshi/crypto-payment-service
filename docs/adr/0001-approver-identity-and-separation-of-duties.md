# Approver identity comes from a trusted header, and an Approver can never be the Customer

Approve, reject, and cancel are identity-sensitive actions on a money-movement resource. We decided that the actor's identity reaches this service via the `X-Actor-ID` HTTP header, injected by the upstream gateway after authentication (authentication itself is owned by the IAM service, out of scope per CONTEXT.md), and that the service enforces the authorization contract derived from the domain model:

- **Cancel**: the actor must be the Customer who owns the Withdrawal.
- **Approve / Reject**: the actor is recorded as the Approver in the audit event metadata, and must *not* be the owning Customer (separation of duties — the person requesting a withdrawal can never approve their own).

## Considered Options

- **`customer_id` in the request body** (previous state): trusts the caller to name any identity. Anyone could approve any withdrawal by passing its customer ID, and it conflated the Approver with the Customer — a direct contradiction of the glossary.
- **Full authn/authz inside this service** (JWT validation, roles): rejected. CONTEXT.md assigns authentication and identity verification to a separate IAM service; duplicating it here would muddy the boundary the demo is trying to teach.
- **mTLS / service identity**: the right production hardening for service-to-service calls, but it changes deployment topology, not the trust contract. The header contract stays identical behind mTLS.

## Consequences

- The demo's honesty rule applies: the header is trusted blindly because the gateway/IAM boundary is stubbed. This must be stated wherever the API is documented.
- Request bodies for approve/reject no longer carry identity. The audit trail gains a real second identity (Approver), which is the point of Phase 7.
- A 403 response exists for separation-of-duties violations; 401 for a missing actor header.
