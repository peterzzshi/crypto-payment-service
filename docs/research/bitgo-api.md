# BitGo API integration research

Research date: 2026-09-28. Sources are limited to the official BitGo Developer
Portal, BitGo's published OpenAPI specification, and the first-party BitGoJS
repository.

## Conclusion

A real BitGo **read-side** integration is manageable in Go: authenticate REST
requests, fetch transfers by ID or `sequenceId`, read `state`, `confirmations`,
and `txid`, and verify wallet-webhook notifications. A generic direct-REST
**broadcast** implementation is not just an HTTP adapter. The documented send
endpoint accepts a half-signed transaction; self-custody signing is normally
performed locally by BitGo Express/BitGoJS. Custody MPC instead uses transaction
requests and operators, and BitGo says custody-wallet transactions remain
unsigned in testnet. [Send endpoint][send] [Self-custody manual flow][self]
[Custody MPC flow][custody]

Recommended project scope:

1. Implement the REST authentication, transfer lookup, and real wallet-webhook
   contract in Go.
2. Use the withdrawal UUID as BitGo `sequenceId`; reconcile an ambiguous send by
   looking it up before retrying.
3. Choose the wallet product before implementing sends. Either run BitGo Express
   for a testnet self-custody wallet, or model custody transaction requests and
   be explicit that testnet cannot prove final custody signing/broadcast.

## Authentication and environments

- Test and production use the same APIs but separate accounts and data. Base
  URLs are `https://app.bitgo-test.com` and `https://app.bitgo.com`.
  [Environments][env]
- REST authentication uses BitGo access tokens. Long-lived tokens (up to ten
  years) are intended for integrations; BitGo recommends rotation, least
  privilege, and a spending limit. Production long-lived tokens require IP/CIDR
  restrictions by default. [Access tokens][tokens]
- A plain Go HTTP client is supported in principle, but current V2/V3 token
  requests require BitGo HMAC authentication in addition to the bearer header.
  For V2 the signed subject is `timestamp|URL path including query|body`; V3 is
  `METHOD|timestamp|3.0|URL path including query|body`. HMAC is SHA-256 keyed by
  the raw access token. The first-party SDK sends `Auth-Timestamp`,
  `BitGo-Auth-Version`, `HMAC`, and `Authorization: Bearer <sha256(token)>`, then
  validates response HMAC and timestamp. [HMAC guide][hmac]
  [BitGoJS HMAC source][hmac-source] [BitGoJS request source][request-source]
- The portal's cURL examples often show `Authorization: Bearer <raw token>` and
  omit the HMAC headers, while the HMAC guide says direct V2+ API calls must
  construct them. Implement against the HMAC guide/first-party SDK and confirm
  with a test-account request rather than copying an isolated cURL example.

## Creating transfers

- Coin and token operations use BitGo's asset identifier in the URL, for
  example `/api/v2/{coin}/wallet/{walletId}/...`. First-party statics currently
  identify native assets as `btc`/`tbtc4` and `eth`/testnet variants, Ethereum
  USDT as `usdt` (test entry `tusdt`), Tron USDT as `trx:usdt`/`ttrx:usdt`, and
  BSC USDT as `bsc:usdt`. Do not derive these strings from this project's
  `(currency, network)` names; keep an explicit configuration mapping.
  [BitGoJS Ethereum tokens][eth-token] [BitGoJS Tron tokens][tron-token]
  [BitGoJS BSC tokens][bsc-token]
- Amounts are base-unit integers. A transaction recipient has at least
  `address` and `amount` (number or string). ETH/ERC20 requests may also set gas
  or EIP-1559 fields. [OpenAPI][openapi]
- `POST /api/v2/{coin}/wallet/{walletId}/tx/send` is specifically documented as
  sending a **half-signed** transaction to BitGo for final signing and broadcast;
  it requires `halfSigned` or `txHex`. It is not a provider-side replacement for
  local transaction construction and user-key signing. [Send endpoint][send]
- Self-custody multisig's documented manual sequence is build via
  `/tx/build`, sign locally (BitGo Express or SDK), then submit the half-signed
  transaction. [Self-custody manual flow][self]
- Go Account's simple flow is one local BitGo Express call, not one direct cloud
  REST call. Its manual flow builds through REST but signs the payload locally
  with Express/BitGoJS before sending. Go Account asset IDs use the `ofc` prefix.
  [Go Account simple flow][go-simple] [Go Account manual flow][go-manual]
- Custody MPC uses transaction requests and wallet-admin/operator approvals.
  BitGo states custody transactions can only complete in production; testnet
  custody transactions remain unsigned. A production custody integration and
  its credentials/contract are therefore outside a credential-free portfolio
  test. [Custody MPC flow][custody]

## Idempotency and lookup

- Supply a stable `sequenceId` on every send. BitGo describes it as optional but
  highly recommended: only one send per sequence ID is confirmed, later sends
  fail, and this permits retrying without double spending. It is wallet-scoped
  and private. [OpenAPI][openapi]
- Resolve timeouts/ambiguous failures with
  `GET /api/v2/{coin}/wallet/{walletId}/transfer/sequenceId/{sequenceId}` before
  attempting another send. Transfers can also be fetched by BitGo transfer ID at
  `GET /api/v2/{coin}/wallet/{walletId}/transfer/{transferId}`.
  [Sequence lookup][sequence-lookup] [Transfer lookup][transfer-lookup]
- `requestId` in error/metadata schemas is a diagnostic client-request ID, not
  the documented money-movement idempotency contract. Use `sequenceId` for send
  deduplication.

## Status and confirmations

- A fetched transfer exposes `state`, `confirmations`, `txid`, `confirmedTime`,
  and `sequenceId`. `confirmations` is the number of blocks confirmed since the
  transfer's block was confirmed. [OpenAPI][openapi]
- Relevant states include `initialized`, `pendingApproval`, `rejected`, `signed`,
  `unconfirmed`, `confirmed`, `removed`, `failed`, and `replaced`. Do not map
  `signed` to this service's `CONFIRMING`: BitGo defines `unconfirmed` as sent to
  the network and awaiting on-chain validation. Treat `removed` (reorg) and
  `replaced` explicitly during reconciliation. [OpenAPI][openapi]
- Go Account/OFC withdrawals have a front transfer and a backing on-chain
  transfer. The top-level `txid` can be internal; poll to `state == confirmed`
  and use `metadata[].onChainTxId` for on-chain reconciliation.
  [Transfer lookup][transfer-lookup]

## Wallet webhooks and signature verification

- Register a `transfer` wallet webhook. Actual notification bodies are compact
  references, not full transfer objects: the documented simulation includes
  `wallet`, `coin`, `transfer`, `state`, `webhook`, and `idempotencyKey`. BitGo
  strongly recommends fetching the transfer by ID before processing it.
  [Wallet webhooks][wallet-webhooks]
- Notification delivery is at-least-once: BitGo retries non-200 responses up to
  seven additional times. Deduplicate using the notification's
  `idempotency-key`/`idempotencyKey`, while the transfer ID remains the domain
  lookup key. [Webhook overview][webhook-overview]
- General wallet/enterprise notification authentication uses an enterprise
  webhook secret and the `X-Signature-SHA256` header. The official verification
  API is `POST /api/v2/webhook/{webhookId}/verify` with the header signature and
  the **raw notification payload encoded as a JSON string**; it returns
  `isValid`. Preserve the exact request bytes before JSON decoding.
  [Webhook verification][webhook-verify]
- BitGo says the signature is HMAC-SHA256 over the payload using the webhook
  secret, but the public guide does not give a language-neutral local test vector
  or an exact canonicalization rule. The safest first milestone is the official
  verification endpoint plus captured sandbox fixtures. A later local Go verifier
  should only replace it after matching those fixtures with constant-time
  comparison. The repository's current generic `X-Signature` contract does not
  match the documented `X-Signature-SHA256` header.
- Do not confuse notification secrets with **policy webhook signing keys**.
  Policy webhooks are a separate two-way approval mechanism using signed JWTs,
  BitGo JWKS, `kid`/`iss` validation, and a customer-signed response. Registering
  a policy signing key requires an enterprise owner and OTP. [Policy signing
  keys][policy-keys]

## Credential and support constraints

- Required for a live sandbox: a BitGo test account/enterprise, long-lived access
  token, wallet ID(s), funded testnet wallet(s), webhook ID and enterprise
  webhook secret, plus a publicly reachable HTTPS callback URL.
- Self-custody sending additionally needs local user-key signing material or a
  BitGo Express signer. This service should not accept a wallet passphrase in its
  public API or log/store it.
- Production custody requires a production customer relationship and operational
  approval/signing flows. Testnet cannot demonstrate completed custody MPC
  withdrawals. BitGo also notes that withdrawing from a custody wallet in the
  test environment requires contacting BitGo. [Environments][env] [Custody MPC
  flow][custody]
- Testnet token coverage is not equivalent to production. BitGo notes most
  chains do not provide testnet tokens, so each proposed asset/network pair must
  be checked in the current official asset catalog and proven with a funded
  wallet; do not promise BTC, ETH, Tron USDT, and BSC USDT all in one sandbox test
  before that evidence exists. [Environments][env]

[env]: https://developers.bitgo.com/docs/get-started-environments
[tokens]: https://developers.bitgo.com/docs/get-started-access-tokens
[hmac]: https://developers.bitgo.com/docs/hmac
[openapi]: https://assets.bitgo.com/api/openapi
[send]: https://developers.bitgo.com/reference/v2wallettxsend
[transfer-lookup]: https://developers.bitgo.com/reference/v2walletgettransfer
[sequence-lookup]: https://developers.bitgo.com/reference/v2walletgettransferbysequenceid
[wallet-webhooks]: https://developers.bitgo.com/docs/webhooks-wallet
[webhook-overview]: https://developers.bitgo.com/docs/webhooks-overview
[webhook-verify]: https://developers.bitgo.com/reference/v2webhooknotificationverify
[policy-keys]: https://developers.bitgo.com/docs/policies-webhook-signing-keys
[self]: https://developers.bitgo.com/docs/withdraw-wallet-type-self-custody-multisig-manual
[custody]: https://developers.bitgo.com/docs/withdraw-wallet-type-custody-mpc
[go-simple]: https://developers.bitgo.com/docs/withdraw-wallet-type-go-account-simple
[go-manual]: https://developers.bitgo.com/docs/withdraw-wallet-type-go-account-manual
[hmac-source]: https://github.com/BitGo/BitGoJS/blob/8f292b3d0e1a000422a874e0281e0b6f4e5ca309/modules/sdk-hmac/src/hmac.ts
[request-source]: https://github.com/BitGo/BitGoJS/blob/8f292b3d0e1a000422a874e0281e0b6f4e5ca309/modules/sdk-api/src/bitgoAPI.ts#L522-L559
[eth-token]: https://github.com/BitGo/BitGoJS/blob/8f292b3d0e1a000422a874e0281e0b6f4e5ca309/modules/statics/src/coins/erc20Coins.ts#L6525-L6535
[tron-token]: https://github.com/BitGo/BitGoJS/blob/8f292b3d0e1a000422a874e0281e0b6f4e5ca309/modules/statics/src/allCoinsAndTokens.ts#L6386-L6394
[bsc-token]: https://github.com/BitGo/BitGoJS/blob/8f292b3d0e1a000422a874e0281e0b6f4e5ca309/modules/statics/src/coins/bscTokens.ts#L156-L163
