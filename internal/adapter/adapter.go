package adapter

import (
	"math/big"

	"crypto-payment-service/internal/domain"
)

// Normalised represents a normalized webhook payload ready for the service layer
type Normalised struct {
	Deposit             domain.IncomingDeposit
	Address             string             // The destination address that received funds
	TransactionMetadata map[string]any
}

// CustodyAdapter normalizes webhooks from a custody provider (e.g., BitGo, Fireblocks)
type CustodyAdapter interface {
	NormaliseIncomingTransaction(payload []byte) (*Normalised, error)
	ValidateAddress(currency domain.Currency, network domain.Network, address string) error
}

// CustodyAdapterWrapper wraps a CustodyAdapter to implement the legacy ChainAdapter interface
// This allows new custody adapters to work with the existing DepositIngestor
type CustodyAdapterWrapper struct {
	custody CustodyAdapter
	asset   domain.Asset // For legacy compatibility
}

func NewCustodyAdapterWrapper(custody CustodyAdapter, asset domain.Asset) *CustodyAdapterWrapper {
	return &CustodyAdapterWrapper{
		custody: custody,
		asset:   asset,
	}
}

func (w *CustodyAdapterWrapper) NormaliseIncomingTransaction(payload []byte) (*Normalised, error) {
	return w.custody.NormaliseIncomingTransaction(payload)
}

func (w *CustodyAdapterWrapper) Asset() domain.Asset {
	return w.asset
}

func (w *CustodyAdapterWrapper) RequiredConfirmations() int {
	// Not used - the custody adapter determines confirmations from its config
	return 0
}

var _ ChainAdapter = (*CustodyAdapterWrapper)(nil)


// ============================================================================
// Legacy adapter types (Phase 2 and earlier - kept for backward compatibility)
// ============================================================================

// ChainAdapter is the legacy single-chain adapter interface
type ChainAdapter interface {
	NormaliseIncomingTransaction(payload []byte) (*Normalised, error)
	Asset() domain.Asset
	RequiredConfirmations() int
}

// BaseAdapter provides common functionality for legacy chain-specific adapters
type BaseAdapter struct {
	asset                 domain.Asset
	requiredConfirmations int
}

func NewBaseAdapter(asset domain.Asset, requiredConfirmations int) BaseAdapter {
	return BaseAdapter{
		asset:                 asset,
		requiredConfirmations: requiredConfirmations,
	}
}

func (b BaseAdapter) Asset() domain.Asset {
	return b.asset
}

func (b BaseAdapter) RequiredConfirmations() int {
	return b.requiredConfirmations
}

// BuildIncomingDeposit is a helper for legacy adapters
func (b BaseAdapter) BuildIncomingDeposit(
	externalTxID string,
	txHash string,
	amountAtomic *big.Int,
	confirmations int,
	metadata map[string]any,
) *domain.IncomingDeposit {
	txHashPtr := &txHash
	// Extract currency and network from the deprecated Asset field
	currency, network := domain.AssetToCurrencyNetwork(b.asset)
	return &domain.IncomingDeposit{
		ExternalTxID:          externalTxID,
		TxHash:                txHashPtr,
		Currency:              currency,
		Network:               network,
		Asset:                 b.asset,
		AmountAtomic:          amountAtomic,
		Confirmations:         confirmations,
		RequiredConfirmations: b.requiredConfirmations,
		TransactionMetadata:   metadata,
	}
}
