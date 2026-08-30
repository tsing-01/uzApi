package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// UserLoginIP holds the schema definition for the UserLoginIP entity.
//
// 记录用户登录过的客户端 IP。同一用户最多允许 domain.UserLoginIPLimit 个不同 IP，
// 超出的 IP 在登录时被拦截。用户绑定新的激活码时会清空自己的记录。
type UserLoginIP struct {
	ent.Schema
}

func (UserLoginIP) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "user_login_ips"},
	}
}

func (UserLoginIP) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("user_id").
			Comment("用户 ID"),
		field.String("ip").
			MaxLen(45).
			NotEmpty().
			Comment("客户端 IP（IPv4/IPv6）"),
		field.Time("created_at").
			Immutable().
			Default(time.Now).
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}).
			Comment("首次从该 IP 登录的时间"),
		field.Time("last_seen_at").
			Default(time.Now).
			UpdateDefault(time.Now).
			SchemaType(map[string]string{dialect.Postgres: "timestamptz"}).
			Comment("最近一次从该 IP 登录的时间"),
	}
}

func (UserLoginIP) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).
			Ref("login_ips").
			Field("user_id").
			Unique().
			Required(),
	}
}

func (UserLoginIP) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "ip").Unique(),
		index.Fields("user_id"),
	}
}
