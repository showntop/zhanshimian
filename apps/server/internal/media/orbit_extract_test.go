package media

import (
	"bytes"
	"context"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestOrbitFrameTimesSixteen(t *testing.T) {
	duration := 3500 * time.Millisecond
	times := OrbitFrameTimes(duration, 16)
	if len(times) != 16 {
		t.Fatalf("len=%d", len(times))
	}
	if times[0] != 0 {
		t.Fatalf("first=%s", times[0])
	}
	if times[15] != duration*15/16 {
		t.Fatalf("last=%s want %s", times[15], duration*15/16)
	}
	if times[15] >= duration {
		t.Fatal("last frame must not hit EOF")
	}
}

func TestOrbitYawSixteen(t *testing.T) {
	if OrbitYaw(0, 16) != 0 || math.Abs(OrbitYaw(1, 16)-22.5) > 1e-9 || OrbitYaw(8, 16) != 180 {
		t.Fatalf("yaw 0/1/8 = %v %v %v", OrbitYaw(0, 16), OrbitYaw(1, 16), OrbitYaw(8, 16))
	}
}

func TestFFMPEGExtractorJPEG(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not found")
	}

	dir := t.TempDir()
	in := filepath.Join(dir, "bars.mp4")
	cmd := exec.Command("ffmpeg", "-y", "-f", "lavfi", "-i", "testsrc=duration=1:size=320x240:rate=30", "-pix_fmt", "yuv420p", in)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("generate fixture: %v: %s", err, stderr.String())
	}
	video, err := os.ReadFile(in)
	if err != nil {
		t.Fatal(err)
	}

	frames, err := NewFFMPEGExtractor().Extract(context.Background(), video, time.Second, OrbitFrameCount)
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != OrbitFrameCount {
		t.Fatalf("len=%d", len(frames))
	}
	jpegSOI := []byte{0xFF, 0xD8}
	for i, f := range frames {
		if len(f.JPEG) < 2 || f.JPEG[0] != jpegSOI[0] || f.JPEG[1] != jpegSOI[1] {
			t.Fatalf("frame %d not JPEG SOI", i)
		}
	}
}
