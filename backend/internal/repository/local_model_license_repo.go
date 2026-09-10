package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/uzapi/internal/service"
)

type localModelLicenseRepository struct{ db *sql.DB }
type localModelLicenseStore struct {
	tx     *sql.Tx
	userID int64
}

func NewLocalModelLicenseRepository(db *sql.DB) service.LocalModelLicenseRepository {
	return &localModelLicenseRepository{db: db}
}

func (r *localModelLicenseRepository) WithUser(ctx context.Context, userID int64, fn func(*service.User, service.LocalModelLicenseStore) error) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	u := &service.User{ID: userID}
	err = tx.QueryRowContext(ctx, `SELECT status,email,password_hash,local_model_access_unlocked_at,local_model_access_revoked_at,local_model_access_version FROM users WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, userID).
		Scan(&u.Status, &u.Email, &u.PasswordHash, &u.LocalModelAccessUnlockedAt, &u.LocalModelAccessRevokedAt, &u.LocalModelAccessVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return service.ErrUserNotFound
	}
	if err != nil {
		return err
	}
	if err = fn(u, &localModelLicenseStore{tx: tx, userID: userID}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *localModelLicenseStore) ChallengeCounts(ctx context.Context, now time.Time) (int, int, error) {
	// Per-user cleanup follows the user lock, avoiding cross-user lock inversions.
	_, err := s.tx.ExecContext(ctx, `DELETE FROM local_model_challenges WHERE user_id=$1 AND expires_at<$2`, s.userID, now.Add(-time.Hour))
	if err != nil {
		return 0, 0, err
	}
	var recent, pending int
	err = s.tx.QueryRowContext(ctx, `SELECT COUNT(*) FILTER (WHERE created_at>$2),COUNT(*) FILTER (WHERE consumed_at IS NULL AND expires_at>$3) FROM local_model_challenges WHERE user_id=$1`, s.userID, now.Add(-time.Minute), now).Scan(&recent, &pending)
	return recent, pending, err
}

func (s *localModelLicenseStore) CreateChallenge(ctx context.Context, c *service.LocalModelChallenge) error {
	key, err := json.Marshal(c.PublicKey)
	if err != nil {
		return err
	}
	_, err = s.tx.ExecContext(ctx, `INSERT INTO local_model_challenges(token_hash,user_id,action,device_id,public_key,key_thumbprint,grant_version,token_version,previous_license_hash,created_at,expires_at) VALUES($1,$2,$3,NULLIF($4,''),$5,$6,$7,$8,$9,$10,$11)`, c.TokenHash, s.userID, c.Action, c.DeviceID, string(key), c.KeyThumbprint, c.GrantVersion, c.TokenVersion, c.PreviousLicenseHash, c.CreatedAt, c.ExpiresAt)
	return err
}

func (s *localModelLicenseStore) GetChallenge(ctx context.Context, hash string) (*service.LocalModelChallenge, error) {
	c := &service.LocalModelChallenge{}
	var key []byte
	err := s.tx.QueryRowContext(ctx, `SELECT token_hash,user_id,action,COALESCE(device_id,''),public_key,key_thumbprint,grant_version,token_version,previous_license_hash,created_at,expires_at,consumed_at FROM local_model_challenges WHERE token_hash=$1 AND user_id=$2 FOR UPDATE`, hash, s.userID).
		Scan(&c.TokenHash, &c.UserID, &c.Action, &c.DeviceID, &key, &c.KeyThumbprint, &c.GrantVersion, &c.TokenVersion, &c.PreviousLicenseHash, &c.CreatedAt, &c.ExpiresAt, &c.ConsumedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrLocalProofInvalid
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(key, &c.PublicKey); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *localModelLicenseStore) ConsumeChallenge(ctx context.Context, hash string, now time.Time) error {
	result, err := s.tx.ExecContext(ctx, `UPDATE local_model_challenges SET consumed_at=$3 WHERE token_hash=$1 AND user_id=$2 AND consumed_at IS NULL AND expires_at>$3`, hash, s.userID, now)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return service.ErrLocalProofInvalid
	}
	return nil
}

const localDeviceColumns = `id,user_id,name,public_key,key_thumbprint,created_at,last_issued_at,revoked_at`

type localDeviceScanner interface{ Scan(...any) error }

func scanLocalDevice(row localDeviceScanner) (*service.LocalModelDevice, error) {
	d := &service.LocalModelDevice{}
	var key []byte
	if err := row.Scan(&d.ID, &d.UserID, &d.Name, &key, &d.KeyThumbprint, &d.CreatedAt, &d.LastIssuedAt, &d.RevokedAt); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(key, &d.PublicKey); err != nil {
		return nil, err
	}
	return d, nil
}

func (s *localModelLicenseStore) Devices(ctx context.Context) ([]service.LocalModelDevice, error) {
	rows, err := s.tx.QueryContext(ctx, `SELECT `+localDeviceColumns+` FROM local_model_devices WHERE user_id=$1 ORDER BY created_at DESC,id`, s.userID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	devices := []service.LocalModelDevice{}
	for rows.Next() {
		d, err := scanLocalDevice(rows)
		if err != nil {
			return nil, err
		}
		devices = append(devices, *d)
	}
	return devices, rows.Err()
}
func (s *localModelLicenseStore) Device(ctx context.Context, id string) (*service.LocalModelDevice, error) {
	d, err := scanLocalDevice(s.tx.QueryRowContext(ctx, `SELECT `+localDeviceColumns+` FROM local_model_devices WHERE id=$1 AND user_id=$2`, id, s.userID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrLocalDeviceNotFound
	}
	return d, err
}
func (s *localModelLicenseStore) DeviceByKey(ctx context.Context, thumb string) (*service.LocalModelDevice, error) {
	d, err := scanLocalDevice(s.tx.QueryRowContext(ctx, `SELECT `+localDeviceColumns+` FROM local_model_devices WHERE user_id=$1 AND key_thumbprint=$2`, s.userID, thumb))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return d, err
}
func (s *localModelLicenseStore) RegistrationsSince(ctx context.Context, since time.Time) (int, error) {
	var n int
	err := s.tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM local_model_license_events WHERE user_id=$1 AND event='device_registered' AND created_at>$2`, s.userID, since).Scan(&n)
	return n, err
}
func (s *localModelLicenseStore) CreateDevice(ctx context.Context, d *service.LocalModelDevice) error {
	key, err := json.Marshal(d.PublicKey)
	if err != nil {
		return err
	}
	_, err = s.tx.ExecContext(ctx, `INSERT INTO local_model_devices(id,user_id,name,public_key,key_thumbprint,created_at) VALUES($1,$2,$3,$4,$5,$6)`, d.ID, s.userID, d.Name, string(key), d.KeyThumbprint, d.CreatedAt)
	return err
}
func (s *localModelLicenseStore) TouchDevice(ctx context.Context, id string, now time.Time) error {
	_, err := s.tx.ExecContext(ctx, `UPDATE local_model_devices SET last_issued_at=$3 WHERE id=$1 AND user_id=$2`, id, s.userID, now)
	return err
}
func (s *localModelLicenseStore) RevokeDevice(ctx context.Context, id string, now time.Time) (bool, error) {
	d, err := s.Device(ctx, id)
	if err != nil {
		return false, err
	}
	if d.RevokedAt != nil {
		return false, nil
	}
	_, err = s.tx.ExecContext(ctx, `UPDATE local_model_devices SET revoked_at=$3 WHERE id=$1 AND user_id=$2`, id, s.userID, now)
	if err != nil {
		return false, err
	}
	_, err = s.tx.ExecContext(ctx, `UPDATE local_model_challenges SET consumed_at=$3 WHERE user_id=$1 AND key_thumbprint=$2 AND consumed_at IS NULL`, s.userID, d.KeyThumbprint, now)
	return true, err
}
func (s *localModelLicenseStore) RevokeEntitlement(ctx context.Context, now time.Time) error {
	_, err := s.tx.ExecContext(ctx, `UPDATE users SET local_model_access_revoked_at=$2,local_model_access_version=local_model_access_version+1,updated_at=$2 WHERE id=$1`, s.userID, now)
	if err != nil {
		return err
	}
	_, err = s.tx.ExecContext(ctx, `UPDATE local_model_devices SET revoked_at=$2 WHERE user_id=$1 AND revoked_at IS NULL`, s.userID, now)
	if err != nil {
		return err
	}
	_, err = s.tx.ExecContext(ctx, `UPDATE local_model_challenges SET consumed_at=$2 WHERE user_id=$1 AND consumed_at IS NULL`, s.userID, now)
	return err
}
func (s *localModelLicenseStore) Audit(ctx context.Context, actorID int64, event, deviceID, reason string, now time.Time) error {
	_, err := s.tx.ExecContext(ctx, `INSERT INTO local_model_license_events(user_id,device_id,actor_id,event,reason,created_at) VALUES($1,NULLIF($2,''),$3,$4,$5,$6)`, s.userID, deviceID, actorID, event, reason, now)
	return err
}
