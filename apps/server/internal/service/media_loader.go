package service

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/zhanshimian/server/internal/domain"
	"github.com/zhanshimian/server/internal/provider"
	"github.com/zhanshimian/server/internal/repository"
	"github.com/zhanshimian/server/internal/storage"
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
	constrain := constrainVisionImage
	process := visionCOSProcess
	if provider.ImageBudget(ctx) == provider.ImageBudgetEdit {
		constrain = constrainEditImage
		process = editCOSProcess
	}
	images := make([]provider.AnalysisImage, 0, len(assets))
	for _, asset := range assets {
		reader, url, err := l.open(ctx, asset, process)
		if err != nil {
			return nil, err
		}
		data, readErr := io.ReadAll(io.LimitReader(reader, l.maxBytes+1))
		closeErr := reader.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read %s photo: %w", string(asset.Purpose), readErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close %s photo: %w", string(asset.Purpose), closeErr)
		}
		if int64(len(data)) > l.maxBytes {
			return nil, fmt.Errorf("%s photo exceeds provider limit", string(asset.Purpose))
		}
		data, mime := constrain(data, asset.MIMEType)
		images = append(images, provider.AnalysisImage{
			ID: asset.ID, Kind: string(asset.Purpose), MIMEType: mime,
			URL: url, Data: data,
		})
	}
	return images, nil
}

// open resolves one asset to a reader and its public URL. Demo photos are
// bundled with the server (see demoMediaAssetPath) and never exist in object
// storage, so they are read from the local asset directory instead.
func (l *AnalysisMediaLoader) open(ctx context.Context, asset domain.MediaAsset, process string) (io.ReadCloser, string, error) {
	if strings.HasPrefix(asset.ObjectKey, "demo/") {
		if l.assetDir == "" {
			return nil, "", fmt.Errorf("open %s photo: demo asset directory is not configured", string(asset.Purpose))
		}
		path := demoMediaAssetPath(string(asset.Purpose))
		file, err := os.Open(filepath.Join(l.assetDir, filepath.FromSlash(strings.TrimPrefix(path, "/assets/"))))
		if err != nil {
			return nil, "", fmt.Errorf("open %s photo: %w", string(asset.Purpose), err)
		}
		return file, l.publicBaseURL + path, nil
	}
	publicURL := l.publicBaseURL + "/uploads/" + strings.TrimPrefix(asset.ObjectKey, "/")
	if processed, ok := l.storage.(storage.ProcessedOpener); ok {
		if reader, err := processed.OpenProcessed(ctx, asset.ObjectKey, process); err == nil {
			return reader, publicURL, nil
		}
	}
	reader, err := l.storage.Open(ctx, asset.ObjectKey)
	if err != nil {
		return nil, "", fmt.Errorf("open %s photo: %w", string(asset.Purpose), err)
	}
	return reader, publicURL, nil
}
