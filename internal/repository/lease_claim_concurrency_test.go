package repository

import (
	"context"
	"fmt"
	"math/big"
	"os"
	"sync"
	"testing"
	"time"

	"crypto-payment-service/ent/enttest"
	"crypto-payment-service/internal/domain"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// TestLeaseClaimByStatus_ConcurrentRace verifies that when N workers race to
// lease-claim the same CONFIRMING withdrawals, each row is leased to exactly
// one worker, an immediate second claim finds nothing, and an expired lease
// becomes reclaimable (ADR-0002).
//
// NOTE: This test requires PostgreSQL because SQLite does not support
// SELECT ... FOR UPDATE. To run:
//
//	docker run -d -p 5432:5432 -e POSTGRES_PASSWORD=test postgres:15
//	TEST_DB_URL="postgres://postgres:test@localhost:5432/postgres?sslmode=disable" go test -race -v -run TestLeaseClaimByStatus_ConcurrentRace ./internal/repository/
func TestLeaseClaimByStatus_ConcurrentRace(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping PostgreSQL contention test in short mode")
	}

	dbURL := os.Getenv("TEST_DB_URL")
	if dbURL == "" {
		t.Skip("TEST_DB_URL not set - skipping concurrency test (requires PostgreSQL)")
	}

	client := enttest.Open(t, "postgres", dbURL)
	defer client.Close()

	repo := NewWithdrawalRepo(client)
	ctx := context.Background()

	customerID := uuid.NewString()
	customer, err := client.Customer.Create().
		SetID(customerID).
		SetExternalID("ext-" + customerID).
		SetEmail(customerID + "@example.com").
		Save(ctx)
	require.NoError(t, err)

	numWithdrawals := 5
	for i := 0; i < numWithdrawals; i++ {
		w := &domain.Withdrawal{
			ID:                    uuid.New().String(),
			CustomerID:            customer.ID,
			IdempotencyKey:        uuid.New().String(),
			DestinationAddress:    "0xDEST",
			Currency:              "ETH",
			Network:               "ethereum",
			Asset:                 domain.AssetETH,
			AmountAtomic:          big.NewInt(1000000),
			Status:                domain.WithdrawalStatusConfirming,
			RequiredConfirmations: 12,
			Version:               1,
		}
		require.NoError(t, repo.Create(ctx, w))
	}

	numWorkers := 10
	batchSize := 10
	leaseTTL := time.Minute

	type claimResult struct {
		workerID string
		claimed  []*domain.Withdrawal
		err      error
	}

	results := make(chan claimResult, numWorkers)
	var wg sync.WaitGroup

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			workerID := fmt.Sprintf("worker-%d", id)
			claimed, err := repo.LeaseClaimByStatus(ctx, domain.WithdrawalStatusConfirming, batchSize, workerID, leaseTTL)
			results <- claimResult{workerID: workerID, claimed: claimed, err: err}
		}(i)
	}

	wg.Wait()
	close(results)

	// Each withdrawal is leased to exactly one worker.
	leasedTo := make(map[string]string)
	for res := range results {
		require.NoError(t, res.err)
		for _, w := range res.claimed {
			require.Equal(t, res.workerID, *w.LockedBy, "returned row must carry the claiming worker's lease")
			require.NotNil(t, w.LockedUntil)
			if prev, exists := leasedTo[w.ID]; exists {
				t.Fatalf("withdrawal %s leased to both %s and %s", w.ID, prev, res.workerID)
			}
			leasedTo[w.ID] = res.workerID
		}
	}
	require.Equal(t, numWithdrawals, len(leasedTo), "not all withdrawals were leased")

	// An immediate second claim by a new worker finds nothing: all leases are
	// unexpired.
	again, err := repo.LeaseClaimByStatus(ctx, domain.WithdrawalStatusConfirming, batchSize, "worker-late", leaseTTL)
	require.NoError(t, err)
	require.Empty(t, again, "unexpired leases must block reclaiming")

	// Expire the leases artificially; a new worker can now reclaim every row.
	_, err = client.Withdrawal.Update().
		SetLockedUntil(time.Now().Add(-time.Second)).
		Save(ctx)
	require.NoError(t, err)

	reclaimed, err := repo.LeaseClaimByStatus(ctx, domain.WithdrawalStatusConfirming, batchSize, "worker-reaper", leaseTTL)
	require.NoError(t, err)
	require.Len(t, reclaimed, numWithdrawals, "expired leases must be reclaimable")
	for _, w := range reclaimed {
		require.Equal(t, "worker-reaper", *w.LockedBy)
	}
}
