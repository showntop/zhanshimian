package service

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/example/jianwo/server/internal/provider"
	"github.com/example/jianwo/server/internal/repository"
	"github.com/example/jianwo/server/internal/storage"
)

type AnalysisMediaLoader struct {
	repo          repository.Repository
	storage       storage.ObjectStorage
	publicBaseURL string
	maxBytes      int64
}

func NewAnalysisMediaLoader(repo repository.Repository, objects storage.ObjectStorage, publicBaseURL string, maxBytes int64) *AnalysisMediaLoader {
	return &AnalysisMediaLoader{repo: repo, storage: objects, publicBaseURL: strings.TrimSuffix(publicBaseURL, "/"), maxBytes: maxBytes}
}

func (l *AnalysisMediaLoader) Load(ctx context.Context, ids []string) ([]provider.AnalysisImage, error) {
	assets, err := l.repo.GetMediaAssets(ctx, ids)
	if err != nil {
		return nil, err
	}
	images := make([]provider.AnalysisImage, 0, len(assets))
	for _, asset := range assets {
		reader, err := l.storage.Open(ctx, asset.StorageKey)
		if err != nil {
			return nil, fmt.Errorf("open %s photo: %w", asset.Kind, err)
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
			URL: l.publicBaseURL + "/uploads/" + strings.TrimPrefix(asset.StorageKey, "/"), Data: data,
		})
	}
	return images, nil
}
