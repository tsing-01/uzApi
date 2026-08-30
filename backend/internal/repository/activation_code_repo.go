package repository

import (
	"context"
	"strings"
	"time"

	dbent "github.com/uzapi/ent"
	"github.com/uzapi/ent/activationcode"
	"github.com/uzapi/internal/domain"
	"github.com/uzapi/internal/pkg/pagination"
	"github.com/uzapi/internal/service"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
)

type activationCodeRepository struct {
	client *dbent.Client
}

// NewActivationCodeRepository 创建激活码仓储。
func NewActivationCodeRepository(client *dbent.Client) service.ActivationCodeRepository {
	return &activationCodeRepository{client: client}
}

func (r *activationCodeRepository) Create(ctx context.Context, code *service.ActivationCode) error {
	client := clientFromContext(ctx, r.client)
	builder := client.ActivationCode.Create().
		SetCode(code.Code).
		SetAmount(code.Amount).
		SetStatus(code.Status).
		SetNotes(code.Notes)
	if code.StartsAt != nil {
		builder.SetStartsAt(*code.StartsAt)
	}
	if code.ExpiresAt != nil {
		builder.SetExpiresAt(*code.ExpiresAt)
	}

	created, err := builder.Save(ctx)
	if err != nil {
		return err
	}
	code.ID = created.ID
	code.CreatedAt = created.CreatedAt
	code.UpdatedAt = created.UpdatedAt
	return nil
}

func (r *activationCodeRepository) CreateBatch(ctx context.Context, codes []service.ActivationCode) error {
	if len(codes) == 0 {
		return nil
	}
	client := clientFromContext(ctx, r.client)
	builders := make([]*dbent.ActivationCodeCreate, 0, len(codes))
	for i := range codes {
		builder := client.ActivationCode.Create().
			SetCode(codes[i].Code).
			SetAmount(codes[i].Amount).
			SetStatus(codes[i].Status).
			SetNotes(codes[i].Notes)
		if codes[i].StartsAt != nil {
			builder.SetStartsAt(*codes[i].StartsAt)
		}
		if codes[i].ExpiresAt != nil {
			builder.SetExpiresAt(*codes[i].ExpiresAt)
		}
		builders = append(builders, builder)
	}

	created, err := client.ActivationCode.CreateBulk(builders...).Save(ctx)
	if err != nil {
		return err
	}
	for i := range created {
		codes[i].ID = created[i].ID
		codes[i].CreatedAt = created[i].CreatedAt
		codes[i].UpdatedAt = created[i].UpdatedAt
	}
	return nil
}

func (r *activationCodeRepository) GetByID(ctx context.Context, id int64) (*service.ActivationCode, error) {
	m, err := r.client.ActivationCode.Query().
		Where(activationcode.IDEQ(id)).
		Only(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, service.ErrActivationCodeNotFound
		}
		return nil, err
	}
	return activationCodeEntityToService(m), nil
}

func (r *activationCodeRepository) GetByCode(ctx context.Context, code string) (*service.ActivationCode, error) {
	m, err := r.client.ActivationCode.Query().
		Where(activationcode.CodeEqualFold(code)).
		Only(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, service.ErrActivationCodeNotFound
		}
		return nil, err
	}
	return activationCodeEntityToService(m), nil
}

func (r *activationCodeRepository) GetByCodeForUpdate(ctx context.Context, code string) (*service.ActivationCode, error) {
	client := clientFromContext(ctx, r.client)
	q := client.ActivationCode.Query().Where(activationcode.CodeEqualFold(code))
	// SQLite（单元测试）不支持 SELECT ... FOR UPDATE；此时依赖 Bind 的乐观锁保证互斥。
	if client.Driver().Dialect() == dialect.Postgres {
		q = q.ForUpdate()
	}
	m, err := q.Only(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, service.ErrActivationCodeNotFound
		}
		return nil, err
	}
	return activationCodeEntityToService(m), nil
}

func (r *activationCodeRepository) Update(ctx context.Context, code *service.ActivationCode) error {
	client := clientFromContext(ctx, r.client)
	builder := client.ActivationCode.UpdateOneID(code.ID).
		SetAmount(code.Amount).
		SetStatus(code.Status).
		SetNotes(code.Notes)
	if code.StartsAt != nil {
		builder.SetStartsAt(*code.StartsAt)
	} else {
		builder.ClearStartsAt()
	}
	if code.ExpiresAt != nil {
		builder.SetExpiresAt(*code.ExpiresAt)
	} else {
		builder.ClearExpiresAt()
	}

	updated, err := builder.Save(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return service.ErrActivationCodeNotFound
		}
		return err
	}
	code.UpdatedAt = updated.UpdatedAt
	return nil
}

func (r *activationCodeRepository) Delete(ctx context.Context, id int64) error {
	client := clientFromContext(ctx, r.client)
	n, err := client.ActivationCode.Delete().Where(activationcode.IDEQ(id)).Exec(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		return service.ErrActivationCodeNotFound
	}
	return nil
}

func (r *activationCodeRepository) List(
	ctx context.Context,
	params pagination.PaginationParams,
	status, search string,
) ([]service.ActivationCode, *pagination.PaginationResult, error) {
	q := r.client.ActivationCode.Query()
	if status != "" {
		q = q.Where(activationcode.StatusEQ(status))
	}
	if search != "" {
		q = q.Where(activationcode.Or(
			activationcode.CodeContainsFold(search),
			activationcode.NotesContainsFold(search),
		))
	}

	total, err := q.Clone().Count(ctx)
	if err != nil {
		return nil, nil, err
	}

	codesQuery := q.
		Offset(params.Offset()).
		Limit(params.Limit())
	for _, order := range activationCodeListOrder(params) {
		codesQuery = codesQuery.Order(order)
	}

	codes, err := codesQuery.All(ctx)
	if err != nil {
		return nil, nil, err
	}

	return activationCodeEntitiesToService(codes), paginationResultFromTotal(int64(total), params), nil
}

func activationCodeListOrder(params pagination.PaginationParams) []func(*entsql.Selector) {
	sortBy := strings.ToLower(strings.TrimSpace(params.SortBy))
	sortOrder := params.NormalizedSortOrder(pagination.SortOrderDesc)

	var field string
	switch sortBy {
	case "amount":
		field = activationcode.FieldAmount
	case "status":
		field = activationcode.FieldStatus
	case "starts_at":
		field = activationcode.FieldStartsAt
	case "expires_at":
		field = activationcode.FieldExpiresAt
	case "used_at":
		field = activationcode.FieldUsedAt
	case "code":
		field = activationcode.FieldCode
	case "created_at":
		field = activationcode.FieldCreatedAt
	default:
		field = activationcode.FieldID
	}

	if sortOrder == pagination.SortOrderAsc {
		return []func(*entsql.Selector){dbent.Asc(field), dbent.Asc(activationcode.FieldID)}
	}
	return []func(*entsql.Selector){dbent.Desc(field), dbent.Desc(activationcode.FieldID)}
}

func (r *activationCodeRepository) GetCurrentByUser(ctx context.Context, userID int64) (*service.ActivationCode, error) {
	client := clientFromContext(ctx, r.client)
	m, err := client.ActivationCode.Query().
		Where(
			activationcode.UsedByEQ(userID),
			activationcode.StatusEQ(domain.ActivationCodeStatusUsed),
		).
		Order(dbent.Desc(activationcode.FieldUsedAt), dbent.Desc(activationcode.FieldID)).
		First(ctx)
	if err != nil {
		if dbent.IsNotFound(err) {
			return nil, service.ErrActivationCodeNotFound
		}
		return nil, err
	}
	return activationCodeEntityToService(m), nil
}

// Bind 通过 WHERE status = 'unused' 的乐观锁把激活码绑定给用户，
// 并发下只有一个请求能成功，其余返回 ErrActivationCodeUsed。
func (r *activationCodeRepository) Bind(ctx context.Context, id, userID int64, usedAt time.Time) error {
	client := clientFromContext(ctx, r.client)
	n, err := client.ActivationCode.Update().
		Where(
			activationcode.IDEQ(id),
			activationcode.StatusEQ(domain.ActivationCodeStatusUnused),
		).
		SetStatus(domain.ActivationCodeStatusUsed).
		SetUsedBy(userID).
		SetUsedAt(usedAt).
		Save(ctx)
	if err != nil {
		return err
	}
	if n == 0 {
		return service.ErrActivationCodeUsed
	}
	return nil
}

func (r *activationCodeRepository) ReplacePrevious(ctx context.Context, userID, exceptID int64) (int, error) {
	client := clientFromContext(ctx, r.client)
	return client.ActivationCode.Update().
		Where(
			activationcode.UsedByEQ(userID),
			activationcode.StatusEQ(domain.ActivationCodeStatusUsed),
			activationcode.IDNEQ(exceptID),
		).
		SetStatus(domain.ActivationCodeStatusReplaced).
		Save(ctx)
}

func activationCodeEntityToService(m *dbent.ActivationCode) *service.ActivationCode {
	if m == nil {
		return nil
	}
	return &service.ActivationCode{
		ID:        m.ID,
		Code:      m.Code,
		Amount:    m.Amount,
		Status:    m.Status,
		StartsAt:  m.StartsAt,
		ExpiresAt: m.ExpiresAt,
		UsedBy:    m.UsedBy,
		UsedAt:    m.UsedAt,
		Notes:     derefString(m.Notes),
		CreatedAt: m.CreatedAt,
		UpdatedAt: m.UpdatedAt,
	}
}

func activationCodeEntitiesToService(models []*dbent.ActivationCode) []service.ActivationCode {
	out := make([]service.ActivationCode, 0, len(models))
	for i := range models {
		if s := activationCodeEntityToService(models[i]); s != nil {
			out = append(out, *s)
		}
	}
	return out
}
