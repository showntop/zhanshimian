package media

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

const (
	OrbitFrameCount    = 16
	OrbitMinKeepFrames = 8
	orbitJPEGQuality   = "80"
	orbitMaxEdge       = 720
)

func OrbitFrameTimes(duration time.Duration, n int) []time.Duration {
	if n <= 0 {
		return nil
	}
	out := make([]time.Duration, n)
	for i := 0; i < n; i++ {
		out[i] = duration * time.Duration(i) / time.Duration(n)
	}
	return out
}

func OrbitYaw(i, n int) float64 {
	if n <= 0 {
		return 0
	}
	return 360.0 * float64(i) / float64(n)
}

type Frame struct {
	Yaw  float64
	JPEG []byte
}

type Extractor interface {
	Extract(ctx context.Context, video []byte, duration time.Duration, n int) ([]Frame, error)
}

type ffmpegExtractor struct{}

func NewFFMPEGExtractor() Extractor { return ffmpegExtractor{} }

func (ffmpegExtractor) Extract(ctx context.Context, video []byte, duration time.Duration, n int) ([]Frame, error) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return nil, fmt.Errorf("ffmpeg not found: %w", err)
	}
	dir, err := os.MkdirTemp("", "body-orbit-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	in := filepath.Join(dir, "in.mp4")
	if err := os.WriteFile(in, video, 0o600); err != nil {
		return nil, err
	}
	times := OrbitFrameTimes(duration, n)
	frames := make([]Frame, 0, n)
	scale := fmt.Sprintf("scale='if(gt(iw,ih),%d,-2)':'if(gt(ih,iw),%d,-2)'", orbitMaxEdge, orbitMaxEdge)
	for i, ts := range times {
		out := filepath.Join(dir, fmt.Sprintf("%02d.jpg", i))
		sec := fmt.Sprintf("%.3f", ts.Seconds())
		cmd := exec.CommandContext(ctx, "ffmpeg", "-y", "-ss", sec, "-i", in,
			"-frames:v", "1", "-vf", scale, "-q:v", "2", out)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			return frames, fmt.Errorf("ffmpeg frame %d: %w: %s", i, err, stderr.String())
		}
		jpeg, err := os.ReadFile(out)
		if err != nil {
			return frames, err
		}
		frames = append(frames, Frame{Yaw: OrbitYaw(i, n), JPEG: jpeg})
	}
	return frames, nil
}
