package broadcast

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"

	"crypto-payment-service/internal/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testWithdrawal() *domain.Withdrawal {
	return &domain.Withdrawal{
		ID:                 "w-1",
		CustomerID:         "c-1",
		Currency:           domain.CurrencyBTC,
		Network:            domain.NetworkBitcoin,
		DestinationAddress: "bc1qdestination",
		AmountAtomic:       big.NewInt(100000),
	}
}

func newBitGoTestServer(t *testing.T, handler http.HandlerFunc) (*BitGoBroadcaster, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return NewBitGoBroadcaster(server.URL, "test-token", "wallet-1", false), server
}

func TestBitGoBroadcaster_Broadcast_Success(t *testing.T) {
	b, _ := newBitGoTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/v2/btc/wallet/wallet-1/sendcoins", r.URL.Path)
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))

		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		var req sendCoinsRequest
		require.NoError(t, json.Unmarshal(body, &req))
		assert.Equal(t, "bc1qdestination", req.Address)
		assert.Equal(t, "100000", req.Amount)

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"txid":"tx-abc","state":"signed"}`))
	})

	txHash, err := b.Broadcast(context.Background(), testWithdrawal())
	require.NoError(t, err)
	assert.Equal(t, "tx-abc", txHash)
}

func TestBitGoBroadcaster_Broadcast_TestnetCoin(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{"txid":"tx-abc"}`))
	}))
	defer server.Close()

	b := NewBitGoBroadcaster(server.URL, "tok", "wallet-1", true)
	_, err := b.Broadcast(context.Background(), testWithdrawal())
	require.NoError(t, err)
	assert.Equal(t, "/api/v2/tbtc/wallet/wallet-1/sendcoins", gotPath)
}

func TestBitGoBroadcaster_Broadcast_UnsupportedAsset(t *testing.T) {
	b, _ := newBitGoTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("no HTTP call expected for unsupported asset")
	})

	w := testWithdrawal()
	w.Currency = domain.Currency("DOGE")
	_, err := b.Broadcast(context.Background(), w)
	require.Error(t, err)
	var unsupported domain.UnsupportedAssetError
	assert.True(t, errors.As(err, &unsupported))
}

func TestBitGoBroadcaster_Broadcast_ErrorClassification(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		expectType any
	}{
		{"rate limit is retryable", http.StatusTooManyRequests, `{"error":"rate limit exceeded"}`, domain.RateLimitError{}},
		{"server error is retryable", http.StatusBadGateway, `{"error":"bad gateway"}`, domain.NetworkError{}},
		{"insufficient funds is non-retryable", http.StatusBadRequest, `{"message":"insufficient funds in wallet"}`, domain.HotWalletInsufficientFundsError{}},
		{"other 4xx is a validation error", http.StatusBadRequest, `{"error":"invalid address"}`, domain.TransactionValidationError{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, _ := newBitGoTestServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			})

			_, err := b.Broadcast(context.Background(), testWithdrawal())
			require.Error(t, err)
			assert.IsType(t, tt.expectType, err)
		})
	}
}

func TestBitGoBroadcaster_Confirmations_Success(t *testing.T) {
	b, _ := newBitGoTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/api/v2/btc/tx/tx-abc", r.URL.Path)
		_, _ = w.Write([]byte(`{"confirmations":7}`))
	})
	b.WithCoin("btc")

	confirmations, err := b.Confirmations(context.Background(), "tx-abc")
	require.NoError(t, err)
	assert.Equal(t, 7, confirmations)
}

func TestBitGoBroadcaster_Confirmations_RequiresCoin(t *testing.T) {
	b, _ := newBitGoTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("no HTTP call expected without a configured coin")
	})

	_, err := b.Confirmations(context.Background(), "tx-abc")
	require.Error(t, err)
	var validationErr domain.TransactionValidationError
	assert.True(t, errors.As(err, &validationErr))
}

func TestNewBroadcasterFromEnv(t *testing.T) {
	t.Run("defaults to stub", func(t *testing.T) {
		t.Setenv("BROADCAST_BACKEND", "")
		b, err := NewBroadcasterFromEnv()
		require.NoError(t, err)
		assert.IsType(t, StubBroadcaster{}, b)
	})

	t.Run("unknown backend errors", func(t *testing.T) {
		t.Setenv("BROADCAST_BACKEND", "acme")
		_, err := NewBroadcasterFromEnv()
		require.Error(t, err)
	})

	t.Run("bitgo requires credentials", func(t *testing.T) {
		t.Setenv("BROADCAST_BACKEND", "bitgo")
		t.Setenv("BITGO_API_TOKEN", "")
		t.Setenv("BITGO_WALLET_ID", "")
		_, err := NewBroadcasterFromEnv()
		require.Error(t, err)
	})

	t.Run("bitgo configured", func(t *testing.T) {
		t.Setenv("BROADCAST_BACKEND", "bitgo")
		t.Setenv("BITGO_API_TOKEN", "tok")
		t.Setenv("BITGO_WALLET_ID", "wallet-1")
		t.Setenv("BITGO_TESTNET", "true")
		t.Setenv("BITGO_API_BASE", "")
		t.Setenv("BITGO_COIN", "tbtc")
		b, err := NewBroadcasterFromEnv()
		require.NoError(t, err)
		bitgo, ok := b.(*BitGoBroadcaster)
		require.True(t, ok)
		assert.Equal(t, "https://app.bitgo-test.com", bitgo.apiBase)
		assert.Equal(t, "tbtc", bitgo.coin)
	})
}
