package adapter

import (
	"crypto-payment-service/internal/domain"
)

// Normalised is a webhook payload translated into the service layer's vocabulary.
type Normalised struct {
	Deposit             domain.IncomingDeposit
	Address             string
	TransactionMetadata map[string]any
}

// CustodyAdapter normalizes webhooks from a custody provider (e.g., BitGo, Fireblocks).
type CustodyAdapter interface {
	NormaliseIncomingTransaction(payload []byte) (*Normalised, error)
	ValidateAddress(currency domain.Currency, network domain.Network, address string) error
}
