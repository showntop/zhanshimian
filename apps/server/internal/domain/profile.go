package domain

import (
	"encoding/json"
	"time"
)

type UserProfile struct {
	ID              string
	UserID          string
	Role            string
	HeightCM        int
	Budget          string
	Preferences     json.RawMessage
	Avoidances      json.RawMessage
	CurrentReportID string
	Version         int
	CreatedAt       time.Time
	UpdatedAt       time.Time
}
