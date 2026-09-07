package storage

import "fmt"

type Config struct {
	Provider  string
	LocalRoot string
	COS       COSConfig
}

func New(cfg Config) (ObjectStorage, error) {
	switch cfg.Provider {
	case "local":
		return NewLocal(cfg.LocalRoot)
	case "cos":
		return NewCOS(cfg.COS)
	default:
		return nil, fmt.Errorf("unsupported storage provider %q", cfg.Provider)
	}
}
