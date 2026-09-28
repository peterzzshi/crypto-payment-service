package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"

	"github.com/google/uuid"
)

// Address holds the schema definition for the Address entity.
type Address struct {
	ent.Schema
}

// Fields of the Address.
func (Address) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			Unique().
			Immutable().
			DefaultFunc(func() string { return uuid.New().String() }),
		field.String("customer_id").
			Immutable(),
		field.String("currency").
			MaxLen(10),
		field.String("network").
			MaxLen(20),
		field.String("asset").
			MaxLen(10).
			Optional().
			Comment("Deprecated: use currency + network"),
		field.String("address").
			MaxLen(100),
		field.Time("created_at").
			Immutable().
			Default(time.Now),
	}
}

// Edges of the Address.
func (Address) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("customer", Customer.Type).
			Ref("addresses").
			Field("customer_id").
			Unique().
			Required().
			Immutable(),
		edge.To("deposits", Deposit.Type),
	}
}

// Indexes of the Address.
func (Address) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("currency", "network", "address").Unique(),
		index.Fields("customer_id"),
	}
}
