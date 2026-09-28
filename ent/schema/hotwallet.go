package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"

	"github.com/google/uuid"
)

type HotWallet struct {
	ent.Schema
}

func (HotWallet) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").
			Unique().
			Immutable().
			DefaultFunc(func() string { return uuid.New().String() }),
		field.String("currency").
			MaxLen(10),
		field.String("network").
			MaxLen(20),
		field.String("asset").
			MaxLen(10).
			Optional().
			Comment("Deprecated: use currency + network"),
		field.Int64("next_nonce").
			Default(0),
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

func (HotWallet) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("currency", "network").Unique(),
	}
}
