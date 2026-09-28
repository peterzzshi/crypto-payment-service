package repository

import (
	"context"
	"fmt"

	"crypto-payment-service/ent"
)

type EntTxManager struct {
	client *ent.Client
}

func NewEntTxManager(client *ent.Client) *EntTxManager {
	return &EntTxManager{client: client}
}

type txReposKey struct{}

type TxRepos struct {
	Deposits         DepositRepository
	Withdrawals      WithdrawalRepository
	DepositEvents    DepositEventRepository
	WithdrawalEvents WithdrawalEventRepository
}

func (m *EntTxManager) WithTx(ctx context.Context, fn func(ctx context.Context) error) (err error) {
	tx, err := m.client.Tx(ctx)
	if err != nil {
		return err
	}

	defer func() {
		if r := recover(); r != nil {
			_ = tx.Rollback()
			panic(r)
		}
	}()

	txCtx := context.WithValue(ctx, txReposKey{}, TxRepos{
		Deposits:         NewDepositRepo(tx.Client()),
		Withdrawals:      NewWithdrawalRepo(tx.Client()),
		DepositEvents:    NewDepositEventRepo(tx.Client()),
		WithdrawalEvents: NewWithdrawalEventRepo(tx.Client()),
	})

	if err := fn(txCtx); err != nil {
		if rerr := tx.Rollback(); rerr != nil {
			return fmt.Errorf("%w (rollback failed: %v)", err, rerr)
		}
		return err
	}

	return tx.Commit()
}

func GetTxRepos(ctx context.Context) (TxRepos, bool) {
	repos, ok := ctx.Value(txReposKey{}).(TxRepos)
	return repos, ok
}
