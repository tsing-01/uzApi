package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/uzapi/internal/service"
)

type localModelQuotaRepository struct{ db *sql.DB }
type localModelQuotaStore struct {
	tx     *sql.Tx
	userID int64
}

func NewLocalModelQuotaRepository(db *sql.DB) service.LocalModelQuotaRepository {
	return &localModelQuotaRepository{db: db}
}

func (r *localModelQuotaRepository) WithUser(ctx context.Context, id int64, fn func(*service.User, service.LocalModelQuotaStore) error) error {
	return withLocalModelUser(ctx, r.db, id, func(u *service.User, tx *sql.Tx) error {
		return fn(u, &localModelQuotaStore{tx: tx, userID: id})
	})
}

func (s *localModelQuotaStore) Now(ctx context.Context) (time.Time, error) {
	var now time.Time
	err := s.tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&now)
	return now.UTC(), err
}

func (s *localModelQuotaStore) TakeRateLimit(ctx context.Context, date string, window time.Time) (bool, error) {
	result, err := s.tx.ExecContext(ctx, `INSERT INTO local_model_daily_usage(user_id,quota_date,used,rate_window,rate_count)
VALUES($1,$2,0,$3,1) ON CONFLICT(user_id,quota_date) DO UPDATE SET
rate_count=CASE WHEN local_model_daily_usage.rate_window=$3 THEN local_model_daily_usage.rate_count+1 ELSE 1 END, rate_window=$3
WHERE local_model_daily_usage.rate_window<>$3 OR local_model_daily_usage.rate_count<$4`, s.userID, date, window, service.CustomAPIRateLimit)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}

func (s *localModelQuotaStore) Used(ctx context.Context, date string) (int64, error) {
	var used int64
	err := s.tx.QueryRowContext(ctx, `SELECT used FROM local_model_daily_usage WHERE user_id=$1 AND quota_date=$2`, s.userID, date).Scan(&used)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return used, err
}

func (s *localModelQuotaStore) Decision(ctx context.Context, id string) (*service.LocalModelRequestDecision, error) {
	d := &service.LocalModelRequestDecision{RequestID: id}
	err := s.tx.QueryRowContext(ctx, `SELECT allowed,entitlement_enabled,entitlement_version,used,decided_at,expires_at
FROM local_model_request_decisions WHERE user_id=$1 AND request_id=$2`, s.userID, id).
		Scan(&d.Allowed, &d.Enabled, &d.EntitlementVersion, &d.Used, &d.DecidedAt, &d.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	d.DecidedAt, d.ExpiresAt = d.DecidedAt.UTC(), d.ExpiresAt.UTC()
	return d, err
}

func (s *localModelQuotaStore) SaveDecision(ctx context.Context, date string, d *service.LocalModelRequestDecision) error {
	// Bounded lazy retention: never remove decisions less than seven days old.
	_, err := s.tx.ExecContext(ctx, `DELETE FROM local_model_request_decisions WHERE user_id=$1 AND request_id IN
(SELECT request_id FROM local_model_request_decisions WHERE user_id=$1 AND decided_at<$2 ORDER BY decided_at LIMIT 500)`, s.userID, d.DecidedAt.Add(-7*24*time.Hour))
	if err != nil {
		return err
	}
	_, err = s.tx.ExecContext(ctx, `INSERT INTO local_model_request_decisions(user_id,request_id,quota_date,allowed,entitlement_enabled,entitlement_version,used,decided_at,expires_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, s.userID, d.RequestID, date, d.Allowed, d.Enabled, d.EntitlementVersion, d.Used, d.DecidedAt, d.ExpiresAt)
	if err != nil {
		return err
	}
	if d.Allowed {
		_, err = s.tx.ExecContext(ctx, `UPDATE local_model_daily_usage SET used=used+1 WHERE user_id=$1 AND quota_date=$2`, s.userID, date)
	}
	return err
}
