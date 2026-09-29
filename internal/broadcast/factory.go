package broadcast

import (
	"fmt"
	"os"
	"strings"

	"go.uber.org/zap"
)

// NewStubBroadcaster creates a test broadcaster with 12 confirmations.
func NewStubBroadcaster() Broadcaster {
	return StubBroadcaster{ConfirmationsValue: 12}
}

// MustNewBroadcasterFromEnv resolves the configured backend, falling back to
// the stub when the configuration is invalid so a misconfigured deployment
// never silently stops processing.
func MustNewBroadcasterFromEnv() Broadcaster {
	b, err := NewBroadcasterFromEnv()
	if err != nil {
		zap.L().Warn("invalid broadcaster configuration, falling back to stub", zap.Error(err))
		return NewStubBroadcaster()
	}
	return b
}

// Backend selection environment variables:
//
//	BROADCAST_BACKEND  "stub" (default) | "bitgo"
//	BITGO_API_TOKEN    required when backend=bitgo
//	BITGO_WALLET_ID    required when backend=bitgo
//	BITGO_API_BASE     optional; defaults to the testnet host when
//	                   BITGO_TESTNET=true, else https://app.bitgo.com
//	BITGO_TESTNET      "true" selects testnet coin codes (tbtc, teth)
//	BITGO_COIN         optional; pins the coin code for confirmation polling
//
// The stub remains the default: the BitGo path is written against the public
// API docs but is unverified without testnet credentials, so nothing switches
// over unless explicitly configured.
func NewBroadcasterFromEnv() (Broadcaster, error) {
	backend := strings.ToLower(strings.TrimSpace(os.Getenv("BROADCAST_BACKEND")))
	switch backend {
	case "", "stub":
		return NewStubBroadcaster(), nil
	case "bitgo":
		return newBitGoFromEnv()
	default:
		return nil, fmt.Errorf("unknown BROADCAST_BACKEND %q (want stub|bitgo)", backend)
	}
}

func newBitGoFromEnv() (Broadcaster, error) {
	token := os.Getenv("BITGO_API_TOKEN")
	walletID := os.Getenv("BITGO_WALLET_ID")
	if token == "" || walletID == "" {
		return nil, fmt.Errorf("BROADCAST_BACKEND=bitgo requires BITGO_API_TOKEN and BITGO_WALLET_ID")
	}

	testnet := strings.EqualFold(os.Getenv("BITGO_TESTNET"), "true")
	apiBase := os.Getenv("BITGO_API_BASE")
	if apiBase == "" {
		if testnet {
			apiBase = "https://app.bitgo-test.com"
		} else {
			apiBase = "https://app.bitgo.com"
		}
	}

	b := NewBitGoBroadcaster(apiBase, token, walletID, testnet)
	if coin := os.Getenv("BITGO_COIN"); coin != "" {
		b.WithCoin(coin)
	}
	return b, nil
}
