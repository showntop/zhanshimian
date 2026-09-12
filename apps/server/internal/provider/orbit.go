package provider

import (
	"context"
	"time"
)

const DemoBodyOrbitVersion = "demo-body-orbit-v1"

type OrbitInput struct {
	Body, Face         []byte
	BodyMIME, FaceMIME string
}

type OrbitOutput struct {
	VideoData       []byte
	MIMEType        string
	Duration        time.Duration
	ProviderVersion string
}

type OrbitGenerator interface {
	Generate(context.Context, OrbitInput) (OrbitOutput, error)
}
