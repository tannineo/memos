package teststore

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/usememos/memos/store"
)

func TestResourceStore(t *testing.T) {
	ctx := context.Background()
	ts := NewTestingStore(ctx, t)
	_, err := ts.CreateResource(ctx, &store.Resource{
		CreatorID:    101,
		Filename:     "test.epub",
		Blob:         []byte("test"),
		InternalPath: "",
		ExternalLink: "",
		Type:         "application/epub+zip",
		Size:         637607,
	})
	require.NoError(t, err)

	correctFilename := "test.epub"
	incorrectFilename := "test.png"
	res, err := ts.GetResource(ctx, &store.FindResource{
		Filename: &correctFilename,
	})
	require.NoError(t, err)
	require.Equal(t, correctFilename, res.Filename)
	require.Equal(t, int32(1), res.ID)
	notFoundResource, err := ts.GetResource(ctx, &store.FindResource{
		Filename: &incorrectFilename,
	})
	require.NoError(t, err)
	require.Nil(t, notFoundResource)

	var correctCreatorID int32 = 101
	var incorrectCreatorID int32 = 102
	_, err = ts.GetResource(ctx, &store.FindResource{
		CreatorID: &correctCreatorID,
	})
	require.NoError(t, err)
	notFoundResource, err = ts.GetResource(ctx, &store.FindResource{
		CreatorID: &incorrectCreatorID,
	})
	require.NoError(t, err)
	require.Nil(t, notFoundResource)

	err = ts.DeleteResource(ctx, &store.DeleteResource{
		ID: 1,
	})
	require.NoError(t, err)
	err = ts.DeleteResource(ctx, &store.DeleteResource{
		ID: 2,
	})
	require.NoError(t, err)
}
