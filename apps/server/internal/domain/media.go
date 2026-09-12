package domain

import "time"

type MediaOrigin string
type MediaPurpose string
type MediaState string
type DisplayKind string
type UploadIntentStatus string

const (
	MediaOriginUserUpload       MediaOrigin = "user_upload"
	MediaOriginProviderOutput   MediaOrigin = "provider_output"
	MediaOriginDemo             MediaOrigin = "demo"
	MediaOriginBundledReference MediaOrigin = "bundled_reference"

	MediaPurposeFace            MediaPurpose = "face"
	MediaPurposeSide            MediaPurpose = "side"
	MediaPurposeBody            MediaPurpose = "body"
	MediaPurposeRenderCandidate MediaPurpose = "render_candidate"
	MediaPurposeFeedback        MediaPurpose = "feedback"
	MediaPurposeWardrobe        MediaPurpose = "wardrobe"

	MediaStateQuarantined MediaState = "quarantined"
	MediaStateReady       MediaState = "ready"
	MediaStatePublished   MediaState = "published"
	MediaStateDeleted     MediaState = "deleted"

	DisplayKindOriginal           DisplayKind = "original"
	DisplayKindGeneratedReference DisplayKind = "generated_reference"
	DisplayKindEffectExample      DisplayKind = "effect_example"
	DisplayKindStyleReference     DisplayKind = "style_reference"

	UploadIntentPending   UploadIntentStatus = "pending"
	UploadIntentCompleted UploadIntentStatus = "completed"
	UploadIntentExpired   UploadIntentStatus = "expired"
)

type MediaAsset struct {
	ID                   string
	UserID               string
	Origin               MediaOrigin
	Purpose              MediaPurpose
	ObjectKey            string
	SHA256               string
	MIMEType             string
	ByteSize             int64
	Width                int
	Height               int
	State                MediaState
	DisplayKind          DisplayKind
	ProviderInvocationID string
	CreatedAt            time.Time
	DeletedAt            *time.Time
}

type UploadIntent struct {
	ID                    string
	UserID                string
	Purpose               MediaPurpose
	MIMEType              string
	ByteSize              int64
	SHA256                string
	ObjectKey             string
	Status                UploadIntentStatus
	ExpiresAt             time.Time
	CompletedMediaAssetID string
	Version               int
	CreatedAt             time.Time
	UpdatedAt             time.Time
}

type CreateUploadIntent struct {
	UserID    string
	Purpose   MediaPurpose
	MIMEType  string
	ByteSize  int64
	SHA256    string
	ObjectKey string
	ExpiresAt time.Time
}

type CompleteUploadIntent struct {
	UserID   string
	IntentID string
	Metadata ObjectMetadata
}

type ObjectMetadata struct {
	ObjectKey string
	MIMEType  string
	ByteSize  int64
	SHA256    string
}

type UploadGrant struct {
	Method    string
	URL       string
	Headers   map[string]string
	ExpiresAt time.Time
}
