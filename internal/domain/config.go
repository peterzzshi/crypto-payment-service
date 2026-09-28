package domain

import (
	"fmt"
	"math/big"
)

// ConfirmationTier defines risk-based confirmation requirements
type ConfirmationTier struct {
	Name                  string
	MinAmountAtomic       *big.Int // Inclusive lower bound
	RequiredConfirmations int
}

// AssetConfig holds configuration for a (Currency, Network) pair
type AssetConfig struct {
	Currency              Currency
	Network               Network
	RequiredConfirmations int // Default confirmations
	ContractAddress       *string
	ConfirmationTiers     []ConfirmationTier // Risk-scaled thresholds
}

// GetRequiredConfirmations returns confirmations needed for the given amount
func (c *AssetConfig) GetRequiredConfirmations(amountAtomic *big.Int) int {
	for i := len(c.ConfirmationTiers) - 1; i >= 0; i-- {
		tier := c.ConfirmationTiers[i]
		if amountAtomic.Cmp(tier.MinAmountAtomic) >= 0 {
			return tier.RequiredConfirmations
		}
	}
	return c.RequiredConfirmations
}

// Key returns the unique identifier for this asset config
func (c *AssetConfig) Key() string {
	return fmt.Sprintf("%s:%s", c.Currency, c.Network)
}

// AssetRegistry holds all supported (Currency, Network) pairs
type AssetRegistry struct {
	configs map[string]*AssetConfig
}

// NewAssetRegistry creates a registry with the given configurations
func NewAssetRegistry(configs []*AssetConfig) *AssetRegistry {
	registry := &AssetRegistry{
		configs: make(map[string]*AssetConfig),
	}
	for _, cfg := range configs {
		registry.configs[cfg.Key()] = cfg
	}
	return registry
}

// Get returns the config for a (Currency, Network) pair
func (r *AssetRegistry) Get(currency Currency, network Network) (*AssetConfig, error) {
	key := fmt.Sprintf("%s:%s", currency, network)
	cfg, ok := r.configs[key]
	if !ok {
		return nil, UnsupportedAssetError{Asset: key}
	}
	return cfg, nil
}

// IsSupported checks if a (Currency, Network) pair is configured
func (r *AssetRegistry) IsSupported(currency Currency, network Network) bool {
	_, err := r.Get(currency, network)
	return err == nil
}

// DefaultRegistry returns the production asset configuration
func DefaultRegistry() *AssetRegistry {
	// Token contract addresses
	usdtEthContract := "0xdAC17F958D2ee523a2206206994597C13D831ec7"   // USDT on Ethereum (ERC-20)
	usdtTronContract := "TR7NHqjeKQxGTCi8q8ZY4pL8otSzgjLj6t"         // USDT on Tron (TRC-20)
	usdtBscContract := "0x55d398326f99059fF775485246999027B3197955"    // USDT on BSC (BEP-20)

	return NewAssetRegistry([]*AssetConfig{
		{
			Currency:              CurrencyBTC,
			Network:               NetworkBitcoin,
			RequiredConfirmations: 6,
			ContractAddress:       nil,
			ConfirmationTiers: []ConfirmationTier{
				{Name: "low", MinAmountAtomic: big.NewInt(0), RequiredConfirmations: 3},
				{Name: "medium", MinAmountAtomic: big.NewInt(100_000_000), RequiredConfirmations: 6},      // 1 BTC
				{Name: "high", MinAmountAtomic: big.NewInt(1_000_000_000), RequiredConfirmations: 12},     // 10 BTC
			},
		},
		{
			Currency:              CurrencyETH,
			Network:               NetworkEthereum,
			RequiredConfirmations: 12,
			ContractAddress:       nil,
			ConfirmationTiers: []ConfirmationTier{
				{Name: "low", MinAmountAtomic: big.NewInt(0), RequiredConfirmations: 6},
				{Name: "medium", MinAmountAtomic: new(big.Int).Mul(big.NewInt(10), big.NewInt(1e18)), RequiredConfirmations: 12},   // 10 ETH
				{Name: "high", MinAmountAtomic: new(big.Int).Mul(big.NewInt(100), big.NewInt(1e18)), RequiredConfirmations: 20},   // 100 ETH
			},
		},
		{
			Currency:              CurrencyBNB,
			Network:               NetworkBSC,
			RequiredConfirmations: 15,
			ContractAddress:       nil,
			ConfirmationTiers: []ConfirmationTier{
				{Name: "low", MinAmountAtomic: big.NewInt(0), RequiredConfirmations: 8},
				{Name: "medium", MinAmountAtomic: new(big.Int).Mul(big.NewInt(50), big.NewInt(1e18)), RequiredConfirmations: 15},   // 50 BNB
				{Name: "high", MinAmountAtomic: new(big.Int).Mul(big.NewInt(500), big.NewInt(1e18)), RequiredConfirmations: 25},   // 500 BNB
			},
		},
		{
			Currency:              CurrencyUSDT,
			Network:               NetworkEthereum,
			RequiredConfirmations: 12,
			ContractAddress:       &usdtEthContract,
			ConfirmationTiers: []ConfirmationTier{
				{Name: "low", MinAmountAtomic: big.NewInt(0), RequiredConfirmations: 6},                    // < $10k
				{Name: "medium", MinAmountAtomic: big.NewInt(10_000_000000), RequiredConfirmations: 12},    // $10k - $100k
				{Name: "high", MinAmountAtomic: big.NewInt(100_000_000000), RequiredConfirmations: 20},     // $100k+
			},
		},
		{
			Currency:              CurrencyUSDT,
			Network:               NetworkTron,
			RequiredConfirmations: 19,
			ContractAddress:       &usdtTronContract,
			ConfirmationTiers: []ConfirmationTier{
				{Name: "low", MinAmountAtomic: big.NewInt(0), RequiredConfirmations: 10},                   // < $10k
				{Name: "medium", MinAmountAtomic: big.NewInt(10_000_000000), RequiredConfirmations: 19},    // $10k - $100k
				{Name: "high", MinAmountAtomic: big.NewInt(100_000_000000), RequiredConfirmations: 27},     // $100k+
			},
		},
		{
			Currency:              CurrencyUSDT,
			Network:               NetworkBSC,
			RequiredConfirmations: 15,
			ContractAddress:       &usdtBscContract,
			ConfirmationTiers: []ConfirmationTier{
				{Name: "low", MinAmountAtomic: big.NewInt(0), RequiredConfirmations: 8},                    // < $10k
				{Name: "medium", MinAmountAtomic: big.NewInt(10_000_000000), RequiredConfirmations: 15},    // $10k - $100k
				{Name: "high", MinAmountAtomic: big.NewInt(100_000_000000), RequiredConfirmations: 25},     // $100k+
			},
		},
	})
}
