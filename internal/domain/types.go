package domain

// Currency represents the asset independent of blockchain/network (BTC, ETH, USDT, USD)
type Currency string

const (
	CurrencyBTC  Currency = "BTC"
	CurrencyETH  Currency = "ETH"
	CurrencyUSDT Currency = "USDT"
	CurrencyBNB  Currency = "BNB"
)

// Network represents the blockchain or payment rail
type Network string

const (
	NetworkBitcoin  Network = "bitcoin"
	NetworkEthereum Network = "ethereum"
	NetworkTron     Network = "tron"
	NetworkBSC      Network = "bsc"
)

// SupportedPair represents a configured (Currency, Network) combination
type SupportedPair struct {
	Currency Currency
	Network  Network
}

// Asset is deprecated — use Currency + Network instead.
// Kept temporarily for backward compatibility during migration.
type Asset string

const (
	AssetBTC Asset = "BTC"
	AssetETH Asset = "ETH"
)

// AssetToCurrencyNetwork converts a legacy Asset to Currency and Network
func AssetToCurrencyNetwork(asset Asset) (Currency, Network) {
	switch asset {
	case AssetBTC:
		return CurrencyBTC, NetworkBitcoin
	case AssetETH:
		return CurrencyETH, NetworkEthereum
	default:
		return "", ""
	}
}

type DepositStatus string

const (
	DepositStatusPending    DepositStatus = "PENDING"
	DepositStatusConfirming DepositStatus = "CONFIRMING"
	DepositStatusCompleted  DepositStatus = "COMPLETED"
	DepositStatusFailed     DepositStatus = "FAILED"
)

type WithdrawalStatus string

const (
	WithdrawalStatusPending WithdrawalStatus = "PENDING"
	// WithdrawalStatusApproved indicates the withdrawal has been approved and is
	// ready to be claimed by a worker for broadcast.
	WithdrawalStatusApproved WithdrawalStatus = "APPROVED"
	// WithdrawalStatusRejected indicates the withdrawal was rejected and will not
	// be processed.
	WithdrawalStatusRejected WithdrawalStatus = "REJECTED"
	// WithdrawalStatusBroadcasting is a transient claim state: atomically
	// flipped from APPROVED during claim to prevent double-broadcast. Released
	// back to APPROVED on retryable failure or advanced to CONFIRMING/FAILED.
	WithdrawalStatusBroadcasting WithdrawalStatus = "BROADCASTING"
	WithdrawalStatusConfirming   WithdrawalStatus = "CONFIRMING"
	WithdrawalStatusCompleted    WithdrawalStatus = "COMPLETED"
	WithdrawalStatusFailed       WithdrawalStatus = "FAILED"
	WithdrawalStatusCancelled    WithdrawalStatus = "CANCELLED"
)
