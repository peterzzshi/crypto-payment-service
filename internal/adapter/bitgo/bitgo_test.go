package bitgo

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"crypto-payment-service/internal/adapter"
	"crypto-payment-service/internal/domain"
)

func TestBitGoAdapter_ParseCoinCode(t *testing.T) {
	registry := domain.DefaultRegistry()
	bitgoAdapter := NewBitGoAdapter(registry)

	tests := []struct {
		name             string
		coin             string
		expectedCurrency domain.Currency
		expectedNetwork  domain.Network
		expectError      bool
	}{
		{
			name:             "BTC",
			coin:             "btc",
			expectedCurrency: domain.CurrencyBTC,
			expectedNetwork:  domain.NetworkBitcoin,
			expectError:      false,
		},
		{
			name:             "ETH",
			coin:             "eth",
			expectedCurrency: domain.CurrencyETH,
			expectedNetwork:  domain.NetworkEthereum,
			expectError:      false,
		},
		{
			name:             "USDT on Ethereum",
			coin:             "teth:usdt",
			expectedCurrency: domain.CurrencyUSDT,
			expectedNetwork:  domain.NetworkEthereum,
			expectError:      false,
		},
		{
			name:             "USDT on Tron",
			coin:             "trx:usdt",
			expectedCurrency: domain.CurrencyUSDT,
			expectedNetwork:  domain.NetworkTron,
			expectError:      false,
		},
		{
			name:        "unsupported coin",
			coin:        "unknown",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			currency, network, err := bitgoAdapter.ParseCoinCode(tt.coin)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectedCurrency, currency)
				assert.Equal(t, tt.expectedNetwork, network)
			}
		})
	}
}

func TestBitGoAdapter_NormaliseIncomingTransaction(t *testing.T) {
	registry := domain.DefaultRegistry()
	bitgoAdapter := NewBitGoAdapter(registry)

	tests := []struct {
		name        string
		payload     string
		expectError bool
		validate    func(*testing.T, *adapter.Normalised)
	}{
		{
			name: "valid BTC deposit",
			payload: `{
				"type": "transfer",
				"walletId": "wallet-1",
				"coin": "btc",
				"transferId": "tx-123",
				"hash": "abc123",
				"state": "confirmed",
				"value": "100000000",
				"confirmations": 3,
				"entries": [
					{"address": "1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa", "value": "100000000"}
				]
			}`,
			expectError: false,
			validate: func(t *testing.T, n *adapter.Normalised) {
				assert.Equal(t, domain.CurrencyBTC, n.Deposit.Currency)
				assert.Equal(t, domain.NetworkBitcoin, n.Deposit.Network)
				assert.Equal(t, "tx-123", n.Deposit.ExternalTxID)
				assert.Equal(t, "abc123", *n.Deposit.TxHash)
				assert.Equal(t, 3, n.Deposit.Confirmations)
				assert.Equal(t, "1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa", n.Address)
			},
		},
		{
			name: "valid ETH deposit with risk-based confirmations",
			payload: `{
				"type": "transfer",
				"walletId": "wallet-2",
				"coin": "eth",
				"transferId": "tx-456",
				"hash": "0xdef456",
				"state": "confirmed",
				"value": "150000000000000000000",
				"confirmations": 15,
				"entries": [
					{"address": "0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb4", "value": "150000000000000000000"}
				]
			}`,
			expectError: false,
			validate: func(t *testing.T, n *adapter.Normalised) {
				assert.Equal(t, domain.CurrencyETH, n.Deposit.Currency)
				assert.Equal(t, domain.NetworkEthereum, n.Deposit.Network)
				assert.Equal(t, "tx-456", n.Deposit.ExternalTxID)
				assert.Equal(t, 15, n.Deposit.Confirmations)
				// 150 ETH should trigger high tier (20 confirmations required)
				assert.Equal(t, 20, n.Deposit.RequiredConfirmations)
			},
		},
		{
			name: "invalid JSON",
			payload: `{invalid json}`,
			expectError: true,
		},
		{
			name: "unsupported coin",
			payload: `{
				"coin": "unknown",
				"transferId": "tx-789",
				"value": "1000000",
				"confirmations": 1,
				"entries": [{"address": "addr", "value": "1000000"}]
			}`,
			expectError: true,
		},
		{
			name: "negative amount",
			payload: `{
				"coin": "btc",
				"transferId": "tx-999",
				"value": "-1000000",
				"confirmations": 1,
				"entries": [{"address": "addr", "value": "-1000000"}]
			}`,
			expectError: true,
		},
		{
			name: "no destination address",
			payload: `{
				"coin": "btc",
				"transferId": "tx-888",
				"value": "1000000",
				"confirmations": 1,
				"entries": [{"address": "addr", "value": "-1000000"}]
			}`,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := bitgoAdapter.NormaliseIncomingTransaction([]byte(tt.payload))

			if tt.expectError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				require.NotNil(t, result)
				if tt.validate != nil {
					tt.validate(t, result)
				}
			}
		})
	}
}

func TestBitGoAdapter_ValidateAddress(t *testing.T) {
	registry := domain.DefaultRegistry()
	bitgoAdapter := NewBitGoAdapter(registry)

	tests := []struct {
		name        string
		currency    domain.Currency
		network     domain.Network
		address     string
		expectError bool
	}{
		{
			name:        "valid Bitcoin address",
			currency:    domain.CurrencyBTC,
			network:     domain.NetworkBitcoin,
			address:     "1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa",
			expectError: false,
		},
		{
			name:        "valid Ethereum address",
			currency:    domain.CurrencyETH,
			network:     domain.NetworkEthereum,
			address:     "0x742d35Cc6634C0532925a3b844Bc9e7595f0bEb4",
			expectError: false,
		},
		{
			name:        "valid Tron address",
			currency:    domain.CurrencyUSDT,
			network:     domain.NetworkTron,
			address:     "TXYZopYRdj2D9XRtbG411XZZ3kM5VkAeBf",
			expectError: false,
		},
		{
			name:        "empty address",
			currency:    domain.CurrencyBTC,
			network:     domain.NetworkBitcoin,
			address:     "",
			expectError: true,
		},
		{
			name:        "invalid Bitcoin address length",
			currency:    domain.CurrencyBTC,
			network:     domain.NetworkBitcoin,
			address:     "short",
			expectError: true,
		},
		{
			name:        "invalid Ethereum address format",
			currency:    domain.CurrencyETH,
			network:     domain.NetworkEthereum,
			address:     "742d35Cc6634C0532925a3b844Bc9e7595f0bEb4",
			expectError: true,
		},
		{
			name:        "invalid Tron address format",
			currency:    domain.CurrencyUSDT,
			network:     domain.NetworkTron,
			address:     "0xYZopYRdj2D9XRtbG411XZZ3kM5VkAeBf",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := bitgoAdapter.ValidateAddress(tt.currency, tt.network, tt.address)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestBitGoWebhook_Unmarshal(t *testing.T) {
	payload := `{
		"type": "transfer",
		"walletId": "wallet-1",
		"coin": "btc",
		"transferId": "tx-123",
		"hash": "abc123",
		"state": "confirmed",
		"value": "100000000",
		"confirmations": 6,
		"entries": [
			{"address": "addr1", "value": "-100000000"},
			{"address": "addr2", "value": "100000000"}
		],
		"metadata": {
			"custom_field": "value"
		}
	}`

	var webhook BitGoWebhook
	err := json.Unmarshal([]byte(payload), &webhook)
	require.NoError(t, err)

	assert.Equal(t, "transfer", webhook.Type)
	assert.Equal(t, "wallet-1", webhook.WalletID)
	assert.Equal(t, "btc", webhook.Coin)
	assert.Equal(t, "tx-123", webhook.TransferID)
	assert.Equal(t, "abc123", webhook.Hash)
	assert.Equal(t, "confirmed", webhook.State)
	assert.Equal(t, "100000000", webhook.Value)
	assert.Equal(t, 6, webhook.Confirmations)
	assert.Len(t, webhook.Entries, 2)
	assert.Equal(t, "value", webhook.Metadata["custom_field"])
}
