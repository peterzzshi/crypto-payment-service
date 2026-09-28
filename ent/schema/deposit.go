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

type Deposit struct {
	ent.Schema
}

func (Deposit) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			Unique().
			Immutable().
			DefaultFunc(func() string { return uuid.New().String() }),
		field.String("customer_id").
			Immutable(),
		field.String("address_id"),
		field.String("external_tx_id").
			Unique(),
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

func (Deposit) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("customer", Customer.Type).
			Ref("deposits").
			Field("customer_id").
			Unique().
			Required().
			Immutable(),
		edge.From("address", Address.Type).
			Ref("deposits").
			Field("address_id").
			Unique().
			Required(),
		edge.To("events", DepositEvent.Type).
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (Deposit) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("external_tx_id").Unique(),
		index.Fields("address_id"),
		index.Fields("status"),
		index.Fields("tx_hash"),
		index.Fields("customer_id"),
	}
}
