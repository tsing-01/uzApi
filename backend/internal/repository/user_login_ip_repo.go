package repository

import (
	"context"
	"time"

	dbent "github.com/uzapi/ent"
	"github.com/uzapi/ent/userloginip"
	"github.com/uzapi/internal/service"
)

type userLoginIPRepository struct {
	client *dbent.Client
}

// NewUserLoginIPRepository 创建用户登录 IP 仓储。
func NewUserLoginIPRepository(client *dbent.Client) service.UserLoginIPRepository {
	return &userLoginIPRepository{client: client}
}

func (r *userLoginIPRepository) ListByUser(ctx context.Context, userID int64) ([]service.UserLoginIP, error) {
	client := clientFromContext(ctx, r.client)
	rows, err := client.UserLoginIP.Query().
		Where(userloginip.UserIDEQ(userID)).
		Order(dbent.Asc(userloginip.FieldID)).
		All(ctx)
	if err != nil {
		return nil, err
	}

	out := make([]service.UserLoginIP, 0, len(rows))
	for _, row := range rows {
		out = append(out, service.UserLoginIP{
			ID:         row.ID,
			UserID:     row.UserID,
			IP:         row.IP,
			CreatedAt:  row.CreatedAt,
			LastSeenAt: row.LastSeenAt,
		})
	}
	return out, nil
}

func (r *userLoginIPRepository) Touch(ctx context.Context, userID int64, ip string) (bool, error) {
	client := clientFromContext(ctx, r.client)
	n, err := client.UserLoginIP.Update().
		Where(
			userloginip.UserIDEQ(userID),
			userloginip.IPEQ(ip),
		).
		SetLastSeenAt(time.Now()).
		Save(ctx)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func (r *userLoginIPRepository) Create(ctx context.Context, userID int64, ip string) error {
	client := clientFromContext(ctx, r.client)
	_, err := client.UserLoginIP.Create().
		SetUserID(userID).
		SetIP(ip).
		Save(ctx)
	return err
}

func (r *userLoginIPRepository) DeleteByUser(ctx context.Context, userID int64) (int, error) {
	client := clientFromContext(ctx, r.client)
	return client.UserLoginIP.Delete().
		Where(userloginip.UserIDEQ(userID)).
		Exec(ctx)
}
