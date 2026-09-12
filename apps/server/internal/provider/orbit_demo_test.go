package provider

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestDemoOrbitGeneratorMP4(t *testing.T) {
	gen := NewDemoOrbitGenerator(filepath.Join("..", "..", "assets"))
	out, err := gen.Generate(context.Background(), OrbitInput{})
	if err != nil {
		t.Fatal(err)
	}
	if out.MIMEType != "video/mp4" || !bytes.HasPrefix(out.VideoData, []byte{0x00, 0x00, 0x00}) {
		t.Fatalf("not mp4: mime=%s len=%d", out.MIMEType, len(out.VideoData))
	}
	if out.ProviderVersion != DemoBodyOrbitVersion || out.Duration < 2*time.Second || out.Duration > 6*time.Second {
		t.Fatalf("meta %#v", out)
	}
}
