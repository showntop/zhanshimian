package service

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/example/jianwo/server/internal/domain"
	"github.com/example/jianwo/server/internal/provider"
	"github.com/example/jianwo/server/internal/repository"
	"github.com/example/jianwo/server/internal/storage"
)

type AnalysisMediaLoader struct {
	repo          repository.Repository
	storage       storage.ObjectStorage
	publicBaseURL string
	maxBytes      int64
	assetDir      string
}

func NewAnalysisMediaLoader(repo repository.Repository, objects storage.ObjectStorage, publicBaseURL string, maxBytes int64, assetDir string) *AnalysisMediaLoader {
	return &AnalysisMediaLoader{repo: repo, storage: objects, publicBaseURL: strings.TrimSuffix(publicBaseURL, "/"), maxBytes: maxBytes, assetDir: assetDir}
}

func (l *AnalysisMediaLoader) Load(ctx context.Context, ids []string) ([]provider.AnalysisImage, error) {
	assets, err := l.repo.GetMediaAssets(ctx, ids)
	if err != nil {
		return nil, err
	}
	images := make([]provider.AnalysisImage, 0, len(assets))
	for _, asset := range assets {
		reader, url, err := l.open(ctx, asset)
		if err != nil {
			return nil, err
		}
		data, readErr := io.ReadAll(io.LimitReader(reader, l.maxBytes+1))
		closeErr := reader.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read %s photo: %w", asset.Kind, readErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close %s photo: %w", asset.Kind, closeErr)
		}
		if int64(len(data)) > l.maxBytes {
			return nil, fmt.Errorf("%s photo exceeds provider limit", asset.Kind)
		}
		images = append(images, provider.AnalysisImage{
			ID: asset.ID, Kind: asset.Kind, MIMEType: asset.MIMEType,
			URL: url, Data: data,
		})
	}
	return images, nil
}

// open resolves one asset to a reader and its public URL. Demo photos are
// bundled with the server (see demoMediaAssetPath) and never exist in object
// storage, so they are read from the local asset directory instead.
func (l *AnalysisMediaLoader) open(ctx context.Context, asset domain.MediaAsset) (io.ReadCloser, string, error) {
	if strings.HasPrefix(asset.StorageKey, "demo/") {
		if l.assetDir == "" {
			return nil, "", fmt.Errorf("open %s photo: demo asset directory is not configured", asset.Kind)
		}
		path := demoMediaAssetPath(asset.Kind)
		file, err := os.Open(filepath.Join(l.assetDir, filepath.FromSlash(strings.TrimPrefix(path, "/assets/"))))
		if err != nil {
			return nil, "", fmt.Errorf("open %s photo: %w", asset.Kind, err)
		}
		return file, l.publicBaseURL + path, nil
	}
	reader, err := l.storage.Open(ctx, asset.StorageKey)
	if err != nil {
		return nil, "", fmt.Errorf("open %s photo: %w", asset.Kind, err)
	}
	return reader, l.publicBaseURL + "/uploads/" + strings.TrimPrefix(asset.StorageKey, "/"), nil
}
