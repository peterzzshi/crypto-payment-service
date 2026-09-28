package repository

import (
	"crypto-payment-service/ent"
	"crypto-payment-service/internal/domain"
)

// translateNotFound maps ent's per-entity NotFoundError to the domain-level
// sentinel so callers above the repository layer don't need to know about ent.
func translateNotFound(err error) error {
	if ent.IsNotFound(err) {
		return domain.NotFoundError{}
	}
	return err
}
