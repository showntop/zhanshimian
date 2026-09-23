package domain

import (
	"encoding/json"
	"time"
)

type IdempotencyStatus string
type IdempotencyBeginOutcome string

const (
	IdempotencyInProgress IdempotencyStatus = "in_progress"
	IdempotencyCompleted  IdempotencyStatus = "completed"

	IdempotencyBeginStarted    IdempotencyBeginOutcome = "started"
	IdempotencyBeginReplay     IdempotencyBeginOutcome = "replay"
	IdempotencyBeginInProgress IdempotencyBeginOutcome = "in_progress"
	IdempotencyBeginConflict   IdempotencyBeginOutcome = "conflict"
)

type IdempotencyRecord struct {
	ID                 string
	UserID             string
	Key                string
	RequestFingerprint string
	Status             IdempotencyStatus
	ResponseStatus     int
	ResponseBody       json.RawMessage
	Scope              string
	ResourceID         string
	ExpiresAt          time.Time
	CreatedAt          time.Time
}

type BeginIdempotency struct {
	UserID             string
	Key                string
	RequestFingerprint string
	Scope              string
	ExpiresAt          time.Time
}
