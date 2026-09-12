package service

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCustomAPIRejectsInvalidUUIDBeforeStorage(t *testing.T) {
	s, err := NewLocalModelQuotaService(nil) // Any DB access would panic.
	require.NoError(t, err)
	for _, id := range []string{"", "not-a-uuid", "5941742f37dc46669f97", "5941742f-37dc-1666-9f97-9e3921274b42", "5941742f-37dc-4666-0f97-9e3921274b42", "{5941742f-37dc-4666-9f97-9e3921274b42}", "urn:uuid:5941742f-37dc-4666-9f97-9e3921274b42", strings.Repeat("a", 1000)} {
		out, err := s.Authorize(context.Background(), 1, id)
		require.ErrorIs(t, err, ErrCustomAPIInvalidRequest)
		require.Nil(t, out)
	}
}
