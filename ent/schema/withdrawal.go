package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"

	"github.com/google/uuid"
)

type Withdrawal struct {
	ent.Schema
}

func (Withdrawal) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			Unique().
			Immutable().
			DefaultFunc(func() string { return uuid.New().String() }),
		field.String("customer_id").
			Immutable(),
		field.String("idempotency_key").
			Unique(),
		field.String("destination_address"),
		field.String("tx_hash").
			Optional().
			Nillable(),
		field.String("currency").
			MaxLen(10),
		field.String("network").
			MaxLen(20),
		field.String("asset").
			MaxLen(10).
			Optional().
			Comment("Deprecated: use currency + network"),
		field.String("amount_atomic"),
		field.String("status").
			MaxLen(20),
		field.Int("confirmations").
			Default(0),
		field.Int("required_confirmations"),
		field.Int("retry_count").
			Default(0),
		field.Time("next_retry_at").
			Optional().
			Nillable(),
		field.String("failure_reason").
			Optional().
			Nillable(),
		field.JSON("transaction_metadata", map[string]any{}).
			Optional(),
		field.Int32("version").
			Default(1),
		field.Time("created_at").
			Immutable().
			Default(time.Now),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
	}
}

func (Withdrawal) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("customer", Customer.Type).
			Ref("withdrawals").
			Field("customer_id").
			Unique().
			Required().
			Immutable(),
		edge.To("events", WithdrawalEvent.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (Withdrawal) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("idempotency_key").Unique(),
		index.Fields("status"),
		index.Fields("tx_hash"),
		index.Fields("customer_id"),
	}
}
