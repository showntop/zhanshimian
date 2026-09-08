package provider

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

var (
	ErrAppleUnavailable   = errors.New("apple sign in unavailable")
	ErrAppleTokenRejected = errors.New("apple identity token rejected")
)

// AppleAuthenticator turns a Sign in with Apple identity_token into the
// stable Apple user identifier (the "sub" claim).
type AppleAuthenticator interface {
	VerifyToken(ctx context.Context, identityToken string) (string, error)
}

const (
	appleJWKSURL   = "https://appleid.apple.com/auth/keys"
	appleIssuer    = "https://appleid.apple.com"
	appleJWKSTTL   = time.Hour
	appleClockSkew = time.Minute
)

type appleJWK struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
}

type appleJWKS struct {
	Keys []appleJWK `json:"keys"`
}

// AppleIDVerifier validates identity tokens against Apple's rotating JWKS
// (cached for one hour) and enforces alg/iss/aud/exp. Apple signs identity
// tokens with RS256; the JWKS keys are RSA public keys.
type AppleIDVerifier struct {
	bundleID string
	client   *http.Client

	mu       sync.Mutex
	cachedAt time.Time
	keys     []appleJWK
}

func NewAppleIDVerifier(bundleID string, client *http.Client) (*AppleIDVerifier, error) {
	if strings.TrimSpace(bundleID) == "" {
		return nil, fmt.Errorf("apple bundle id is required")
	}
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	return &AppleIDVerifier{bundleID: strings.TrimSpace(bundleID), client: client}, nil
}

func (v *AppleIDVerifier) VerifyToken(ctx context.Context, identityToken string) (string, error) {
	parts := strings.Split(strings.TrimSpace(identityToken), ".")
	if len(parts) != 3 {
		return "", ErrAppleTokenRejected
	}
	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	headerData, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || json.Unmarshal(headerData, &header) != nil {
		return "", ErrAppleTokenRejected
	}
	if header.Alg != "RS256" || header.Kid == "" {
		return "", ErrAppleTokenRejected
	}
	publicKey, err := v.publicKey(ctx, header.Kid)
	if err != nil {
		return "", err
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return "", ErrAppleTokenRejected
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(publicKey, crypto.SHA256, digest[:], signature); err != nil {
		return "", ErrAppleTokenRejected
	}
	claimsData, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", ErrAppleTokenRejected
	}
	var claims struct {
		Iss string `json:"iss"`
		Aud string `json:"aud"`
		Sub string `json:"sub"`
		Exp int64  `json:"exp"`
	}
	if json.Unmarshal(claimsData, &claims) != nil {
		return "", ErrAppleTokenRejected
	}
	if claims.Iss != appleIssuer || claims.Aud != v.bundleID || claims.Sub == "" {
		return "", ErrAppleTokenRejected
	}
	if time.Now().Add(appleClockSkew).Unix() > claims.Exp {
		return "", ErrAppleTokenRejected
	}
	return claims.Sub, nil
}

// publicKey resolves one key id, refreshing the cached JWKS when it is older
// than an hour or does not contain the requested kid (Apple rotates keys).
func (v *AppleIDVerifier) publicKey(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	v.mu.Lock()
	cached, age := v.keys, time.Since(v.cachedAt)
	v.mu.Unlock()
	if key := findAppleKey(cached, kid); key != nil && age < appleJWKSTTL {
		return key, nil
	}
	fresh, err := v.fetchJWKS(ctx)
	if err != nil {
		// Serve a stale key rather than failing the login when Apple's
		// endpoint hiccups; the signature still has to verify.
		if key := findAppleKey(cached, kid); key != nil {
			return key, nil
		}
		return nil, err
	}
	if key := findAppleKey(fresh, kid); key != nil {
		return key, nil
	}
	return nil, ErrAppleTokenRejected
}

func (v *AppleIDVerifier) fetchJWKS(ctx context.Context) ([]appleJWK, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, appleJWKSURL, nil)
	if err != nil {
		return nil, ErrAppleUnavailable
	}
	response, err := v.client.Do(request)
	if err != nil {
		return nil, ErrAppleUnavailable
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
		return nil, ErrAppleUnavailable
	}
	var payload appleJWKS
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&payload); err != nil || len(payload.Keys) == 0 {
		return nil, ErrAppleUnavailable
	}
	v.mu.Lock()
	v.keys, v.cachedAt = payload.Keys, time.Now()
	v.mu.Unlock()
	return payload.Keys, nil
}

func findAppleKey(keys []appleJWK, kid string) *rsa.PublicKey {
	for _, item := range keys {
		if item.Kid != kid || item.Kty != "RSA" {
			continue
		}
		modulus, err := base64.RawURLEncoding.DecodeString(item.N)
		if err != nil || len(modulus) == 0 {
			return nil
		}
		exponent, err := base64.RawURLEncoding.DecodeString(item.E)
		if err != nil || len(exponent) == 0 {
			return nil
		}
		return &rsa.PublicKey{N: new(big.Int).SetBytes(modulus), E: int(new(big.Int).SetBytes(exponent).Int64())}
	}
	return nil
}
