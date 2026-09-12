package provider

import (
	"context"
	"os"
	"path/filepath"
	"time"
)

type DemoOrbitGenerator struct {
	assetDir string
}

func NewDemoOrbitGenerator(assetDir string) *DemoOrbitGenerator {
	return &DemoOrbitGenerator{assetDir: assetDir}
}

func (g *DemoOrbitGenerator) Generate(_ context.Context, _ OrbitInput) (OrbitOutput, error) {
	data, err := os.ReadFile(filepath.Join(g.assetDir, "demo", "body-orbit.mp4"))
	if err != nil {
		return OrbitOutput{}, err
	}
	return OrbitOutput{
		VideoData:       data,
		MIMEType:        "video/mp4",
		Duration:        3 * time.Second,
		ProviderVersion: DemoBodyOrbitVersion,
	}, nil
}
