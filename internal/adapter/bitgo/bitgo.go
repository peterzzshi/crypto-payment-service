package bitgo

import (
	"encoding/json"
	"fmt"
	"math/big"

	"crypto-payment-service/internal/adapter"
	"crypto-payment-service/internal/domain"
)

// BitGoWebhook represents the webhook payload from BitGo transfer notifications.
// NOTE: This structure is based on expected BitGo webhook format but has not been
// verified against live BitGo webhooks. Field names (especially `transferId`) should
// be confirmed when integrating with actual BitGo webhooks.
type BitGoWebhook struct {
	Type          string                 `json:"type"`
	WalletID      string                 `json:"walletId"`
	Coin          string                 `json:"coin"` // e.g., "btc", "eth", "teth:usdt", "trx:usdt"
	TransferID    string                 `json:"transferId"`
	Hash          string                 `json:"hash"`
	State         string                 `json:"state"`
	Value         string                 `json:"value"` // Atomic amount
	Confirmations int                    `json:"confirmations"`
	Entries       []BitGoTransferEntry   `json:"entries"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
}

type BitGoTransferEntry struct {
	Address string `json:"address"`
	Value   string `json:"value"` // Signed amount (negative = outgoing)
}

// BitGoAdapter handles deposits from BitGo webhooks across all supported chains
type BitGoAdapter struct {
	registry *domain.AssetRegistry
}

func NewBitGoAdapter(registry *domain.AssetRegistry) *BitGoAdapter {
	return &BitGoAdapter{
		registry: registry,
	}
}

// ParseCoinCode extracts (Currency, Network) from BitGo coin code
// Examples: "btc" -> (BTC, bitcoin), "eth" -> (ETH, ethereum), "teth:usdt" -> (USDT, ethereum), "trx:usdt" -> (USDT, tron), "bsc" -> (BNB, bsc), "bsc:usdt" -> (USDT, bsc)
func (a *BitGoAdapter) ParseCoinCode(coin string) (domain.Currency, domain.Network, error) {
	switch coin {
	case "btc":
		return domain.CurrencyBTC, domain.NetworkBitcoin, nil
	case "eth":
		return domain.CurrencyETH, domain.NetworkEthereum, nil
	case "bsc":
		return domain.CurrencyBNB, domain.NetworkBSC, nil
	case "teth:usdt":
		return domain.CurrencyUSDT, domain.NetworkEthereum, nil
	case "trx:usdt":
		return domain.CurrencyUSDT, domain.NetworkTron, nil
	case "bsc:usdt":
		return domain.CurrencyUSDT, domain.NetworkBSC, nil
	default:
		return "", "", fmt.Errorf("unsupported BitGo coin code: %s", coin)
	}
}

// NormaliseIncomingTransaction parses BitGo webhook into domain model
func (a *BitGoAdapter) NormaliseIncomingTransaction(payload []byte) (*adapter.Normalised, error) {
	var webhook BitGoWebhook
	if err := json.Unmarshal(payload, &webhook); err != nil {
		return nil, domain.WebhookBadPayloadError{Err: fmt.Errorf("invalid JSON: %w", err)}
	}

	// Parse coin code to get Currency + Network
	currency, network, err := a.ParseCoinCode(webhook.Coin)
	if err != nil {
		return nil, domain.WebhookBadPayloadError{Err: err}
	}

	// Get asset config for this pair
	config, err := a.registry.Get(currency, network)
	if err != nil {
		return nil, err
	}

	// Parse amount
	amountAtomic, ok := new(big.Int).SetString(webhook.Value, 10)
	if !ok || amountAtomic.Sign() <= 0 {
		return nil, domain.WebhookBadPayloadError{Err: fmt.Errorf("invalid amount: %s", webhook.Value)}
	}

	// Extract destination address (first positive entry)
	var destinationAddress string
	for _, entry := range webhook.Entries {
		entryValue, ok := new(big.Int).SetString(entry.Value, 10)
		if ok && entryValue.Sign() > 0 {
			destinationAddress = entry.Address
			break
		}
	}

	if destinationAddress == "" {
		return nil, domain.WebhookBadPayloadError{Err: fmt.Errorf("no destination address found in entries")}
	}

	// Determine required confirmations based on amount
	requiredConfirmations := config.GetRequiredConfirmations(amountAtomic)

	return &adapter.Normalised{
		Deposit: domain.IncomingDeposit{
			CustomerID:            "", // Resolved by service layer via address lookup
			AddressID:             "", // Resolved by service layer via address lookup
			ExternalTxID:          webhook.TransferID,
			TxHash:                &webhook.Hash,
			Currency:              currency,
			Network:               network,
			AmountAtomic:          amountAtomic,
			Confirmations:         webhook.Confirmations,
			RequiredConfirmations: requiredConfirmations,
		},
		Address:             destinationAddress,
		TransactionMetadata: webhook.Metadata,
	}, nil
}

// ValidateAddress validates address format for the given currency/network
func (a *BitGoAdapter) ValidateAddress(currency domain.Currency, network domain.Network, address string) error {
	// Basic validation — production would use checksum validation (EIP-55 for ETH, base58check for BTC)
	if address == "" {
		return domain.InvalidAddressError{Address: address, Reason: "empty address"}
	}

	switch network {
	case domain.NetworkBitcoin:
		if len(address) < 26 || len(address) > 35 {
			return domain.InvalidAddressError{Address: address, Reason: "invalid Bitcoin address length"}
		}
	case domain.NetworkEthereum, domain.NetworkBSC:
		if len(address) != 42 || address[:2] != "0x" {
			return domain.InvalidAddressError{Address: address, Reason: "invalid EVM address format"}
		}
	case domain.NetworkTron:
		if len(address) != 34 || address[0] != 'T' {
			return domain.InvalidAddressError{Address: address, Reason: "invalid Tron address format"}
		}
	default:
		return domain.UnsupportedAssetError{Asset: fmt.Sprintf("%s:%s", currency, network)}
	}

	return nil
}
