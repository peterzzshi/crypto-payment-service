# Crypto Payment Service

A payment service that ingests and settles crypto deposits and withdrawals on behalf of customers, mirroring a production custody-integrated payments system (Fireblocks/BitGo-style).

## Language

**Currency**:
The crypto asset being transferred, independent of which chain it moves on (e.g. BTC, ETH, USDT, BNB). This system is crypto-only — fiat currencies (USD, EUR) are explicitly out of scope, not merely undeprioritized.
_Avoid_: Asset (the historical term conflated currency and network; split into Currency and Network in Phase 3)

**Network**:
The blockchain a Currency actually moves on (e.g. Ethereum, Tron, BSC, Bitcoin). Technically, any smart-contract-capable Network can host any token Currency — the constraint is never technical. Every crypto Currency has a Network.
_Avoid_: Chain

**Supported Pair**:
A (Currency, Network) combination this system is configured to accept, defined in `AssetRegistry` config rather than hardcoded or technically derived. Since any token can technically exist on any smart-contract chain, "supported" is a pure business/ops decision (which pairs this deployment chooses to handle), not a reflection of blockchain capability. A request outside the configured set is rejected as unsupported. The frontend is expected to only offer configured pairs to customers, so this is a rare-path guard rather than a primary UX concern.
_Avoid_: Asset combination

**Network Mismatch**:
A deposit whose actual origin network doesn't match the Network the receiving address was provisioned for (e.g. a Tron-native transfer landing against an Ethereum-provisioned address). Treated as a terminal, unrecoverable failure — blockchain transfers can't be reversed — so the system records and surfaces it rather than attempting automated recovery. Out of this system's scope: routing it to a support/finance workflow, since this demo has no downstream team to hand off to.
_Avoid_: Wrong network deposit

**Caller**:
Whoever or whatever initiated a transaction request against this service — a player, an affiliate, or an AI agent acting on a principal's behalf. This service processes any authenticated caller's request identically; authentication, authorization, and identity verification (KYC) are owned entirely by a separate IAM service, out of this system's scope.
_Avoid_: User (conflates "caller of this API" with "customer/player" — not every caller is a retail customer)

**Approver**:
An authenticated internal operator or risk service authorized to approve or reject a pending Withdrawal. The Approver is distinct from the Customer who owns the Withdrawal, and both identities belong in its audit history.
_Avoid_: Customer approver, reviewer
