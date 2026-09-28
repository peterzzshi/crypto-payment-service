package main

import (
	"fmt"
	"os"

	"crypto-payment-service/internal/domain"
)

func main() {
	registry := domain.DefaultRegistry()

	fmt.Println("Validating contract addresses...")

	if err := domain.ValidateContractAddresses(registry); err != nil {
		fmt.Fprintf(os.Stderr, "Validation failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("✓ All contract addresses valid")
	fmt.Println("✓ No duplicate addresses detected")
	fmt.Println("✓ EIP-55 checksum validation passed")
}
