package worker

import (
	"context"
	"reflect"
	"testing"
	"time"

	"crypto-payment-service/internal/domain"
)

func TestPipelineProcessAll(t *testing.T) {
	approved := []*domain.Withdrawal{{ID: "approved"}}
	confirmingWithdrawals := []*domain.Withdrawal{{ID: "confirming-withdrawal"}}
	confirmingDeposits := []*domain.Deposit{{ID: "confirming-deposit"}}

	var calls []string
	pipeline := newPipelineForTest(workerFunctions{
		claimApprovedWithdrawals: func(_ context.Context, limit int) ([]*domain.Withdrawal, error) {
			if limit != batchSize {
				t.Fatalf("claim approved limit = %d, want %d", limit, batchSize)
			}
			calls = append(calls, "claim-approved")
			return approved, nil
		},
		claimConfirmingWithdrawals: func(_ context.Context, limit int) ([]*domain.Withdrawal, error) {
			if limit != batchSize {
				t.Fatalf("claim confirming withdrawals limit = %d, want %d", limit, batchSize)
			}
			calls = append(calls, "claim-confirming-withdrawals")
			return confirmingWithdrawals, nil
		},
		claimConfirmingDeposits: func(_ context.Context, limit int) ([]*domain.Deposit, error) {
			if limit != batchSize {
				t.Fatalf("claim confirming deposits limit = %d, want %d", limit, batchSize)
			}
			calls = append(calls, "claim-confirming-deposits")
			return confirmingDeposits, nil
		},
		broadcastApprovedWithdrawals: func(_ context.Context, withdrawals []*domain.Withdrawal) {
			calls = append(calls, "broadcast:"+withdrawals[0].ID)
		},
		processConfirmingWithdrawals: func(_ context.Context, withdrawals []*domain.Withdrawal) {
			calls = append(calls, "process-confirming-withdrawals:"+withdrawals[0].ID)
		},
		processConfirmingDeposits: func(_ context.Context, deposits []*domain.Deposit) {
			calls = append(calls, "process-confirming-deposits:"+deposits[0].ID)
		},
	})

	pipeline.ProcessAll(context.Background())

	want := []string{
		"claim-approved",
		"broadcast:approved",
		"claim-confirming-withdrawals",
		"process-confirming-withdrawals:confirming-withdrawal",
		"claim-confirming-deposits",
		"process-confirming-deposits:confirming-deposit",
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %v, want %v", calls, want)
	}
}

func TestWorkerRunStopsAfterInFlightRound(t *testing.T) {
	roundStarted := make(chan struct{})
	finishRound := make(chan struct{})

	pipeline := newPipelineForTest(workerFunctions{
		claimApprovedWithdrawals: func(context.Context, int) ([]*domain.Withdrawal, error) {
			close(roundStarted)
			<-finishRound
			return nil, nil
		},
		claimConfirmingWithdrawals: func(context.Context, int) ([]*domain.Withdrawal, error) {
			return nil, nil
		},
		claimConfirmingDeposits: func(context.Context, int) ([]*domain.Deposit, error) {
			return nil, nil
		},
		broadcastApprovedWithdrawals: func(context.Context, []*domain.Withdrawal) {},
		processConfirmingWithdrawals: func(context.Context, []*domain.Withdrawal) {},
		processConfirmingDeposits:    func(context.Context, []*domain.Deposit) {},
	})

	worker := &Worker{pipeline: pipeline, interval: time.Millisecond, stopped: make(chan struct{})}
	done := make(chan struct{})
	go worker.Run(done)

	<-roundStarted
	close(done)
	close(finishRound)

	<-worker.Stopped()
}
