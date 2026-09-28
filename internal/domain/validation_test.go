package domain

import (
	"testing"
)

func TestValidateEIP55Checksum(t *testing.T) {
	tests := []struct {
		name    string
		address string
		wantErr bool
	}{
		{
			name:    "valid checksummed address",
			address: "0xdAC17F958D2ee523a2206206994597C13D831ec7",
			wantErr: false,
		},
		{
			name:    "valid all lowercase",
			address: "0xdac17f958d2ee523a2206206994597c13d831ec7",
			wantErr: false,
		},
		{
			name:    "valid all uppercase",
			address: "0xDAC17F958D2EE523A2206206994597C13D831EC7",
			wantErr: false,
		},
		{
			name:    "invalid format - no 0x prefix",
			address: "dAC17F958D2ee523a2206206994597C13D831ec7",
			wantErr: true,
		},
		{
			name:    "invalid format - too short",
			address: "0xdAC17F958D2ee523a220620699",
			wantErr: true,
		},
		{
			name:    "invalid character",
			address: "0xdAC17F958D2ee523a2206206994597C13D831eG7",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateEIP55Checksum(tt.address)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateEIP55Checksum() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateContractAddresses(t *testing.T) {
	t.Run("valid registry with no duplicates", func(t *testing.T) {
		registry := DefaultRegistry()
		if err := ValidateContractAddresses(registry); err != nil {
			t.Errorf("ValidateContractAddresses() unexpected error: %v", err)
		}
	})

	t.Run("detects duplicate addresses", func(t *testing.T) {
		duplicate := "0xdAC17F958D2ee523a2206206994597C13D831ec7"
		registry := NewAssetRegistry([]*AssetConfig{
			{
				Currency:        CurrencyUSDT,
				Network:         NetworkEthereum,
				ContractAddress: &duplicate,
			},
			{
				Currency:        CurrencyUSDT,
				Network:         NetworkBSC,
				ContractAddress: &duplicate,
			},
		})

		err := ValidateContractAddresses(registry)
		if err == nil {
			t.Error("ValidateContractAddresses() expected duplicate error, got nil")
		}
	})

	t.Run("detects invalid checksum", func(t *testing.T) {
		invalid := "0xGGC17F958D2ee523a2206206994597C13D831ec7"
		registry := NewAssetRegistry([]*AssetConfig{
			{
				Currency:        CurrencyUSDT,
				Network:         NetworkEthereum,
				ContractAddress: &invalid,
			},
		})

		err := ValidateContractAddresses(registry)
		if err == nil {
			t.Error("ValidateContractAddresses() expected checksum error, got nil")
		}
	})
}
