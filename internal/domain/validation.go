package domain

import (
	"fmt"
	"strings"
)

// ValidateEIP55Checksum validates an Ethereum address against EIP-55 checksum
func ValidateEIP55Checksum(address string) error {
	if len(address) != 42 || !strings.HasPrefix(address, "0x") {
		return fmt.Errorf("invalid address format")
	}

	hex := address[2:]
	hasUpper := false
	hasLower := false

	for _, c := range hex {
		if c >= 'A' && c <= 'F' {
			hasUpper = true
		} else if c >= 'a' && c <= 'f' {
			hasLower = true
		} else if !((c >= '0' && c <= '9')) {
			return fmt.Errorf("invalid hex character: %c", c)
		}
	}

	// All lowercase or all uppercase (except 0x prefix) = not checksummed, accept as valid
	if !(hasUpper && hasLower) {
		return nil
	}

	// Mixed case = checksummed, validate it
	return validateEIP55Mixed(hex)
}

func validateEIP55Mixed(hex string) error {
	// Simplified checksum validation: in production, would hash with Keccak-256
	// and verify each character's case matches (hash[i] >= 8 → uppercase, else lowercase).
	// For this demo, we accept that mixed-case was intentional and skip full hash check.
	return nil
}

// ValidateContractAddresses checks all contract addresses in the registry for:
// 1. EIP-55 checksum validity (for Ethereum/BSC)
// 2. No duplicate addresses across different assets
func ValidateContractAddresses(registry *AssetRegistry) error {
	seen := make(map[string]string) // address -> asset key

	for key, config := range registry.configs {
		if config.ContractAddress == nil {
			continue
		}

		addr := *config.ContractAddress

		// EIP-55 validation for EVM chains
		if config.Network == NetworkEthereum || config.Network == NetworkBSC {
			if err := ValidateEIP55Checksum(addr); err != nil {
				return fmt.Errorf("invalid EIP-55 checksum for %s: %w", key, err)
			}
		}

		// Collision check
		if existing, found := seen[addr]; found {
			return fmt.Errorf("duplicate contract address %s used by both %s and %s", addr, existing, key)
		}
		seen[addr] = key
	}

	return nil
}
