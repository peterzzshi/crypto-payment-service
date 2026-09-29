package broadcast

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"crypto-payment-service/internal/domain"
)

// BitGoBroadcaster talks to the BitGo REST API over HTTP. It implements the
// same Broadcaster seam as the stub, so the processor and its retry semantics
// are unchanged — only the error classification differs, and it is mapped to
// the same domain error types the state machine already understands.
//
// The payload shapes below follow BitGo's public API docs but are UNVERIFIED
// against a live/testnet account (no credentials in this environment); the
// httptest-based unit tests pin the request/response contract we assume.
type BitGoBroadcaster struct {
	apiBase  string
	apiToken string
	walletID string
	testnet  bool
	coin     string // pinned coin for confirmation polling (a tx hash carries no coin)
	client   *http.Client
}

// NewBitGoBroadcaster builds a broadcaster for one BitGo wallet. apiBase is
// the API root (e.g. https://app.bitgo.com or https://app.bitgo-test.com).
func NewBitGoBroadcaster(apiBase, apiToken, walletID string, testnet bool) *BitGoBroadcaster {
	return &BitGoBroadcaster{
		apiBase:  strings.TrimRight(apiBase, "/"),
		apiToken: apiToken,
		walletID: walletID,
		testnet:  testnet,
		client:   &http.Client{Timeout: 30 * time.Second},
	}
}

// WithCoin pins the BitGo coin code used for confirmation polling. Broadcast
// derives the coin from each withdrawal's (currency, network), but a bare tx
// hash carries no coin, so polling needs a configured value — one hot wallet
// serves one network in practice.
func (b *BitGoBroadcaster) WithCoin(coin string) *BitGoBroadcaster {
	b.coin = coin
	return b
}

// bitGoCoin maps our (Currency, Network) pair to a BitGo coin code. BitGo
// uses distinct codes for testnet coins (tbtc, teth); fiat rails have no
// on-chain representation and are rejected.
func (b *BitGoBroadcaster) bitGoCoin(currency domain.Currency, network domain.Network) (string, error) {
	type pair struct {
		currency domain.Currency
		network  domain.Network
	}
	coins := map[pair]struct{ mainnet, testnet string }{
		{domain.CurrencyBTC, domain.NetworkBitcoin}:  {"btc", "tbtc"},
		{domain.CurrencyETH, domain.NetworkEthereum}: {"eth", "teth"},
	}
	entry, ok := coins[pair{currency, network}]
	if !ok {
		return "", domain.UnsupportedAssetError{Asset: string(currency) + "/" + string(network)}
	}
	if b.testnet {
		return entry.testnet, nil
	}
	return entry.mainnet, nil
}

type sendCoinsRequest struct {
	Address string `json:"address"`
	Amount  string `json:"amount"` // base units (satoshi/wei) as a decimal string
}

type sendCoinsResponse struct {
	TxID     string `json:"txid"`
	Transfer string `json:"transfer"`
	State    string `json:"state"`
}

type txResponse struct {
	Confirmations int `json:"confirmations"`
}

type bitGoErrorBody struct {
	Error   string `json:"error"`
	Message string `json:"message"`
	Name    string `json:"name"`
}

// Broadcast submits the withdrawal as a BitGo sendcoins call and returns the
// resulting tx id. HTTP failures are classified into the domain error types
// the processor's retry logic keys on.
func (b *BitGoBroadcaster) Broadcast(ctx context.Context, withdrawal *domain.Withdrawal) (string, error) {
	coin, err := b.bitGoCoin(withdrawal.Currency, withdrawal.Network)
	if err != nil {
		return "", err
	}

	body, err := json.Marshal(sendCoinsRequest{
		Address: withdrawal.DestinationAddress,
		Amount:  withdrawal.AmountAtomic.String(),
	})
	if err != nil {
		return "", err
	}

	url := fmt.Sprintf("%s/api/v2/%s/wallet/%s/sendcoins", b.apiBase, coin, b.walletID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	var resp sendCoinsResponse
	if err := b.do(req, &resp); err != nil {
		return "", err
	}
	if resp.TxID == "" {
		return "", domain.TransactionValidationError{Err: fmt.Errorf("bitgo response missing txid (state=%s)", resp.State)}
	}
	return resp.TxID, nil
}

// Confirmations fetches the confirmation count for a broadcast transaction.
func (b *BitGoBroadcaster) Confirmations(ctx context.Context, txHash string) (int, error) {
	return b.confirmationsForCoin(ctx, txHash)
}

// confirmationsForCoin queries the transaction by id. The coin code is not
// derivable from a tx hash, so the broadcaster is configured per deployment
// and tries the coins it can serve; in practice a deployment processes one
// network per hot wallet, so BITGO_COIN pins it (see factory).
func (b *BitGoBroadcaster) confirmationsForCoin(ctx context.Context, txHash string) (int, error) {
	if b.coin == "" {
		return 0, domain.TransactionValidationError{Err: fmt.Errorf("bitgo coin not configured for confirmation polling")}
	}
	url := fmt.Sprintf("%s/api/v2/%s/tx/%s", b.apiBase, b.coin, txHash)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}

	var resp txResponse
	if err := b.do(req, &resp); err != nil {
		return 0, err
	}
	return resp.Confirmations, nil
}

// do executes the request with auth, decodes the JSON body into out, and
// classifies failures into domain error types:
//   - 429                         -> RateLimitError (retryable)
//   - transport errors, 5xx       -> NetworkError (retryable)
//   - 4xx "insufficient funds"    -> HotWalletInsufficientFundsError (non-retryable)
//   - other 4xx                   -> TransactionValidationError (non-retryable)
func (b *BitGoBroadcaster) do(req *http.Request, out any) error {
	req.Header.Set("Authorization", "Bearer "+b.apiToken)

	resp, err := b.client.Do(req)
	if err != nil {
		return domain.NetworkError{Err: err}
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return domain.NetworkError{Err: err}
	}

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if out == nil {
			return nil
		}
		if err := json.Unmarshal(payload, out); err != nil {
			return domain.TransactionValidationError{Err: fmt.Errorf("decoding bitgo response: %w", err)}
		}
		return nil
	}

	return classifyHTTPError(resp.StatusCode, payload)
}

func classifyHTTPError(status int, payload []byte) error {
	detail := http.StatusText(status)
	var body bitGoErrorBody
	if err := json.Unmarshal(payload, &body); err == nil {
		switch {
		case body.Message != "":
			detail = body.Message
		case body.Error != "":
			detail = body.Error
		case body.Name != "":
			detail = body.Name
		}
	}
	err := fmt.Errorf("bitgo http %d: %s", status, detail)

	switch {
	case status == http.StatusTooManyRequests:
		return domain.RateLimitError{Err: err}
	case status >= 500:
		return domain.NetworkError{Err: err}
	case status >= 400 && status < 500 && strings.Contains(strings.ToLower(detail), "insufficient"):
		return domain.HotWalletInsufficientFundsError{Err: err}
	default:
		return domain.TransactionValidationError{Err: err}
	}
}
