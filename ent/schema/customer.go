package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"

	"github.com/google/uuid"
)

// Customer holds the schema definition for the Customer entity.
type Customer struct {
	ent.Schema
}

// Fields of the Customer.
func (Customer) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			Unique().
			Immutable().
			DefaultFunc(func() string { return uuid.New().String() }),
		field.String("external_id").
			MaxLen(100).
			Unique(),
		field.String("email").
			MaxLen(255),
		field.Time("created_at").
			Immutable().
			Default(time.Now),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
	}
}

// Edges of the Customer.
func (Customer) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("addresses", Address.Type),
		edge.To("deposits", Deposit.Type),
		edge.To("withdrawals", Withdrawal.Type),
	}
}

// Indexes of the Customer.
func (Customer) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("external_id"),
	}
}
