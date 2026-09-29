package repository

import (
	"context"
	"math/big"
	"os"
	"sync"
	"testing"

	"crypto-payment-service/ent/enttest"
	"crypto-payment-service/internal/domain"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// TestClaimByStatus_ConcurrentRace verifies that when N goroutines race to claim
// the same APPROVED withdrawals, exactly one goroutine wins each row due to
// SELECT ... FOR UPDATE SKIP LOCKED.
//
// NOTE: This test requires PostgreSQL because SQLite does not support
// SELECT ... FOR UPDATE. To run:
//
//	docker run -d -p 5432:5432 -e POSTGRES_PASSWORD=test postgres:15
//	TEST_DB_URL="postgres://postgres:test@localhost:5432/postgres?sslmode=disable" go test -race -v -run TestClaimByStatus_ConcurrentRace ./internal/repository/
func TestClaimByStatus_ConcurrentRace(t *testing.T) {
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

	// Use unique identifiers so repeated runs can share a development database.
	customerID := uuid.NewString()
	customer, err := client.Customer.Create().
		SetID(customerID).
		SetExternalID("ext-" + customerID).
		SetEmail(customerID + "@example.com").
		Save(ctx)
	require.NoError(t, err)

	// Create test withdrawals
	numWithdrawals := 5
	withdrawalIDs := make([]string, numWithdrawals)
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
			Status:                domain.WithdrawalStatusApproved,
			RequiredConfirmations: 12,
			Version:               1,
		}
		require.NoError(t, repo.Create(ctx, w))
		withdrawalIDs[i] = w.ID
	}

	// Race: spawn N goroutines trying to claim all APPROVED withdrawals.
	numGoroutines := 10
	batchSize := 10 // each goroutine tries to claim up to 10 rows

	type claimResult struct {
		goroutineID int
		claimed     []*domain.Withdrawal
		err         error
	}

	results := make(chan claimResult, numGoroutines)
	var wg sync.WaitGroup

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			claimed, err := repo.ClaimForBroadcast(ctx, batchSize)
			results <- claimResult{goroutineID: id, claimed: claimed, err: err}
		}(i)
	}

	wg.Wait()
	close(results)

	// Collect results
	claimedByGoroutine := make(map[int][]string)
	for res := range results {
		require.NoError(t, res.err, "goroutine %d failed", res.goroutineID)
		for _, w := range res.claimed {
			claimedByGoroutine[res.goroutineID] = append(claimedByGoroutine[res.goroutineID], w.ID)
		}
	}

	// Verify: each withdrawal ID appears in exactly one goroutine's result
	seenWithdrawals := make(map[string]int) // withdrawal ID -> goroutine ID that claimed it
	for gid, ids := range claimedByGoroutine {
		for _, wid := range ids {
			if prevGID, exists := seenWithdrawals[wid]; exists {
				t.Fatalf("withdrawal %s claimed by both goroutine %d and %d", wid, prevGID, gid)
			}
			seenWithdrawals[wid] = gid
		}
	}

	// Verify: all withdrawals were claimed by exactly one goroutine
	require.Equal(t, numWithdrawals, len(seenWithdrawals), "not all withdrawals were claimed")

	// Verify: status transitioned to BROADCASTING
	for wid := range seenWithdrawals {
		w, err := repo.GetByID(ctx, wid)
		require.NoError(t, err)
		require.Equal(t, domain.WithdrawalStatusBroadcasting, w.Status,
			"withdrawal %s should be BROADCASTING after claim", wid)
	}
}
