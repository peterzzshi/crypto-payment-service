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

type WithdrawalEvent struct {
	ent.Schema
}

func (WithdrawalEvent) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			Unique().
			Immutable().
			DefaultFunc(func() string { return uuid.New().String() }),
		field.String("withdrawal_id").
			Immutable(),
		field.String("event_type").
			MaxLen(50),
		field.String("from_status").
			MaxLen(20).
			Optional().
			Nillable(),
		field.String("to_status").
			MaxLen(20).
			Optional().
			Nillable(),
		field.JSON("metadata", map[string]any{}).
			Optional(),
		field.Time("created_at").
			Immutable().
			Default(time.Now),
	}
}

func (WithdrawalEvent) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("withdrawal", Withdrawal.Type).
			Ref("events").
			Field("withdrawal_id").
			Unique().
			Required().
			Immutable().
			Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (WithdrawalEvent) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("withdrawal_id"),
		index.Fields("event_type"),
		index.Fields("created_at"),
	}
}
