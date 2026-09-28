package worker

import (
	"context"
	"reflect"
	"testing"
	"time"

	"crypto-payment-service/internal/domain"
)

func TestWorkerProcessAll(t *testing.T) {
	approved := []*domain.Withdrawal{{ID: "approved"}}
	confirmingWithdrawals := []*domain.Withdrawal{{ID: "confirming-withdrawal"}}
	confirmingDeposits := []*domain.Deposit{{ID: "confirming-deposit"}}

	var calls []string
	worker := newWorker(time.Second, workerFunctions{
		claimWithdrawals: func(_ context.Context, status domain.WithdrawalStatus, limit int) ([]*domain.Withdrawal, error) {
			if limit != batchSize {
				t.Fatalf("claim withdrawal limit = %d, want %d", limit, batchSize)
			}
			calls = append(calls, "claim-withdrawals:"+string(status))
			switch status {
			case domain.WithdrawalStatusApproved:
				return approved, nil
			case domain.WithdrawalStatusConfirming:
				return confirmingWithdrawals, nil
			default:
				t.Fatalf("unexpected withdrawal status %q", status)
				return nil, nil
			}
		},
		claimDeposits: func(_ context.Context, status domain.DepositStatus, limit int) ([]*domain.Deposit, error) {
			if status != domain.DepositStatusConfirming {
				t.Fatalf("claim deposit status = %q, want %q", status, domain.DepositStatusConfirming)
			}
			if limit != batchSize {
				t.Fatalf("claim deposit limit = %d, want %d", limit, batchSize)
			}
			calls = append(calls, "claim-deposits:"+string(status))
			return confirmingDeposits, nil
		},
		processPendingWithdrawals: func(_ context.Context, withdrawals []*domain.Withdrawal) {
			calls = append(calls, "process-pending:"+withdrawals[0].ID)
		},
		processConfirmingWithdrawals: func(_ context.Context, withdrawals []*domain.Withdrawal) {
			calls = append(calls, "process-confirming-withdrawals:"+withdrawals[0].ID)
		},
		processConfirmingDeposits: func(_ context.Context, deposits []*domain.Deposit) {
			calls = append(calls, "process-confirming-deposits:"+deposits[0].ID)
		},
	})

	worker.processAll(context.Background())

	want := []string{
		"claim-withdrawals:APPROVED",
		"process-pending:approved",
		"claim-withdrawals:CONFIRMING",
		"process-confirming-withdrawals:confirming-withdrawal",
		"claim-deposits:CONFIRMING",
		"process-confirming-deposits:confirming-deposit",
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
}
