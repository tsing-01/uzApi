package schema

import (
	"time"

	"github.com/uzapi/internal/domain"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// ActivationCode holds the schema definition for the ActivationCode entity.
//
// 激活码：管理员创建，用户在登录后自行添加。一码一人，绑定时把 amount 充入用户余额，
// 并把该用户之前绑定的激活码置为 replaced（被替换）。用户的登录资格由「当前绑定且在
// 有效期内的激活码」决定。
//
// 删除策略：硬删除（与 RedeemCode / PromoCode 一致）
type ActivationCode struct {
	ent.Schema
}

func (ActivationCode) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "activation_codes"},
	}
}

func (ActivationCode) Fields() []ent.Field {
	return []ent.Field{
		field.String("code").
			MaxLen(64).
			NotEmpty().
			Unique().
			Comment("激活码"),
		field.Float("amount").
			SchemaType(map[string]string{dialect.Postgres: "decimal(20,8)"}).
			Default(0).
			Comment("绑定时充入用户余额的金额"),
		field.String("status").
			MaxLen(20).
			Default(domain.ActivationCodeStatusUnused).
			Comment("状态: unused, used, replaced, disabled"),
		field.Time("starts_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}).
			Comment("生效日期，null 表示立即生效"),
		field.Time("expires_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}).
			Comment("失效日期，null 表示永不过期"),
		field.Int64("used_by").
			Optional().
			Nillable().
			Comment("绑定的用户 ID"),
		field.Time("used_at").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}).
			Comment("绑定时间"),
		field.String("notes").
			Optional().
			Nillable().
			SchemaType(map[string]string{dialect.Postgres: "text"}).
			Comment("备注"),
		field.Time("created_at").
			Immutable().
			Default(time.Now).
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now).
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}),
	}
}

func (ActivationCode) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).
			Ref("activation_codes").
			Field("used_by").
			Unique(),
	}
}

func (ActivationCode) Indexes() []ent.Index {
	return []ent.Index{
		// code 字段已在 Fields() 中声明 Unique()，无需重复索引
		index.Fields("status"),
		index.Fields("used_by"),
		index.Fields("expires_at"),
	}
}
