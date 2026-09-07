package service

import "testing"

func TestDetectImageType(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		mime string
		ext  string
	}{
		{name: "png", data: []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}, mime: "image/png", ext: ".png"},
		{name: "jpeg", data: []byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 0x4a, 0x46, 0x49, 0x46, 0x00}, mime: "image/jpeg", ext: ".jpg"},
		{name: "text", data: []byte("not an image"), mime: "text/plain; charset=utf-8", ext: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mime, ext := detectImageType(test.data)
			if mime != test.mime || ext != test.ext {
				t.Fatalf("got %q %q, want %q %q", mime, ext, test.mime, test.ext)
			}
		})
	}
}

func TestBuildToolResult(t *testing.T) {
	tests := []struct {
		kind        string
		wantOptions int
		wantFinding int
	}{
		{kind: "hair", wantOptions: 3},
		{kind: "outfit", wantFinding: 3},
		{kind: "purchase", wantFinding: 3},
	}
	for _, test := range tests {
		t.Run(test.kind, func(t *testing.T) {
			result := buildToolResult(test.kind, "daily")
			if result.Kind != test.kind || result.PriorityTitle == "" || result.PriorityCopy == "" {
				t.Fatalf("incomplete result: %#v", result)
			}
			if len(result.Options) != test.wantOptions || len(result.Findings) != test.wantFinding {
				t.Fatalf("got %d options and %d findings", len(result.Options), len(result.Findings))
			}
		})
	}
}
