package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type localAccessCodeRepo struct {
	RedeemCodeRepository
	created []RedeemCode
}

func (r *localAccessCodeRepo) Create(_ context.Context, code *RedeemCode) error {
	r.created = append(r.created, *code)
	return nil
}
func (r *localAccessCodeRepo) CreateBatch(_ context.Context, codes []RedeemCode) error {
	r.created = append(r.created, codes...)
	return nil
}

func TestLocalModelAccessGenerationValidation(t *testing.T) {
	ctx := context.Background()
	repo := &localAccessCodeRepo{}
	admin := &adminServiceImpl{redeemCodeRepo: repo}
	svc := &RedeemService{redeemRepo: repo}
	generated, err := admin.GenerateRedeemCodes(ctx, &GenerateRedeemCodesInput{Count: 2, Type: RedeemTypeLocalModelAccess})
	require.NoError(t, err)
	require.Len(t, generated, 2)
	require.Len(t, generated[0].Code, 32)
	require.NotEqual(t, generated[0].Code, generated[1].Code)
	for _, code := range generated {
		require.Zero(t, code.Value)
		require.Zero(t, code.ValidityDays)
		require.Nil(t, code.GroupID)
		require.Equal(t, StatusUnused, code.Status)
	}
	batch, err := svc.GenerateCodes(ctx, GenerateCodesRequest{Count: 1, Type: RedeemTypeLocalModelAccess})
	require.NoError(t, err)
	require.Len(t, batch[0].Code, 32)
	groupID := int64(1)
	for _, invalid := range []GenerateRedeemCodesInput{
		{Value: 10}, {Value: -1}, {GroupID: &groupID}, {ValidityDays: 30}, {ValidityDays: -1},
	} {
		invalid.Count = 1
		invalid.Type = RedeemTypeLocalModelAccess
		before := len(repo.created)
		_, err := admin.GenerateRedeemCodes(ctx, &invalid)
		require.Error(t, err)
		err = svc.CreateCode(ctx, &RedeemCode{Code: "test-code", Type: invalid.Type, Value: invalid.Value, GroupID: invalid.GroupID, ValidityDays: invalid.ValidityDays})
		require.Error(t, err)
		require.Len(t, repo.created, before)
	}
}
