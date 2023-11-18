package domains_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/go-shiori/shiori/internal/config"
	"github.com/go-shiori/shiori/internal/dependencies"
	"github.com/go-shiori/shiori/internal/domains"
	"github.com/go-shiori/shiori/internal/mocks"
	"github.com/go-shiori/shiori/internal/model"
	"github.com/psanford/memfs"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestBookmarkDomain(t *testing.T) {
	fs := memfs.New()

	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	deps := &dependencies.Dependencies{
		Database: mocks.NewMockDB(mockCtrl),
		Config:   config.ParseServerConfiguration(context.TODO(), logrus.New()),
		Log:      logrus.New(),
		Domains:  &dependencies.Domains{},
	}
	deps.Domains.Storage = domains.NewStorageDomain(deps, fs)

	fs.MkdirAll("thumb", 0755)
	fs.WriteFile("thumb/1", []byte("x"), 0644)
	fs.MkdirAll("ebook", 0755)
	fs.WriteFile("ebook/1.epub", []byte("x"), 0644)
	fs.MkdirAll("archive", 0755)
	// TODO: write a valid archive file
	fs.WriteFile("archive/1", []byte("x"), 0644)

	domain := domains.NewBookmarksDomain(deps)
	t.Run("HasEbook", func(t *testing.T) {
		t.Run("Yes", func(t *testing.T) {
			require.True(t, domain.HasEbook(&model.BookmarkDTO{ID: 1}))
		})
		t.Run("No", func(t *testing.T) {
			require.False(t, domain.HasEbook(&model.BookmarkDTO{ID: 2}))
		})
	})

	t.Run("HasArchive", func(t *testing.T) {
		t.Run("Yes", func(t *testing.T) {
			require.True(t, domain.HasArchive(&model.BookmarkDTO{ID: 1}))
		})
		t.Run("No", func(t *testing.T) {
			require.False(t, domain.HasArchive(&model.BookmarkDTO{ID: 2}))
		})
	})

	t.Run("GetThumbnailPath", func(t *testing.T) {
		thumbnailPath := domain.GetThumbnailPath(&model.BookmarkDTO{ID: 1})
		require.Equal(t, filepath.Join("thumb/1"), thumbnailPath)
	})

	t.Run("HasThumbnail", func(t *testing.T) {
		t.Run("Yes", func(t *testing.T) {
			require.True(t, domain.HasThumbnail(&model.BookmarkDTO{ID: 1}))
		})
		t.Run("No", func(t *testing.T) {
			require.False(t, domain.HasThumbnail(&model.BookmarkDTO{ID: 2}))
		})
	})

	t.Run("GetBookmark", func(t *testing.T) {
		t.Run("Success", func(t *testing.T) {
			deps.Database.(*mocks.MockDB).EXPECT().
				GetBookmark(gomock.Any(), 1, "").
				Return(model.BookmarkDTO{
					ID:   1,
					HTML: "<p>hello world</p>",
				}, true, nil)
			bookmark, err := domain.GetBookmark(context.Background(), 1)
			require.NoError(t, err)
			require.Equal(t, 1, bookmark.ID)

			// Check DTO attributes
			require.True(t, bookmark.HasEbook)
			require.True(t, bookmark.HasArchive)
		})
	})
}
