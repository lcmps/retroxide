package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	authfeature "github.com/lcmps/retroxide/internal/features/auth"
	"github.com/lcmps/retroxide/internal/shared/fault"
)

const jwtExpiryLeeway = 60 * time.Second

type JWTService struct {
	secret            []byte
	accessExpirySecs  uint64
	refreshExpirySecs uint64
	now               func() time.Time
}

func NewJWTService(secret string, accessExpirySecs, refreshExpirySecs uint64) *JWTService {
	return &JWTService{
		secret:            []byte(secret),
		accessExpirySecs:  accessExpirySecs,
		refreshExpirySecs: refreshExpirySecs,
		now:               time.Now,
	}
}

type jwtHeader struct {
	Algorithm string `json:"alg"`
	Type      string `json:"typ,omitempty"`
}

type jwtClaims struct {
	Subject  string  `json:"sub"`
	Role     string  `json:"role"`
	Kind     string  `json:"kind"`
	Expires  uint64  `json:"exp"`
	IssuedAt uint64  `json:"iat"`
	JTI      *string `json:"jti,omitempty"`
}

func (service *JWTService) Issue(userID, role string) (authfeature.TokenPair, error) {
	now := service.now().Unix()
	if now < 0 {
		return authfeature.TokenPair{}, errors.New("system time predates unix epoch")
	}
	nowSeconds := uint64(now)
	if service.accessExpirySecs > ^uint64(0)-nowSeconds || service.refreshExpirySecs > ^uint64(0)-nowSeconds {
		return authfeature.TokenPair{}, errors.New("token expiration overflows unix timestamp")
	}
	jtiBytes := make([]byte, 16)
	if _, err := rand.Read(jtiBytes); err != nil {
		return authfeature.TokenPair{}, fmt.Errorf("generating refresh token jti: %w", err)
	}
	refreshJTI := hex.EncodeToString(jtiBytes)
	accessToken, err := service.sign(jwtClaims{
		Subject: userID, Role: role, Kind: "access",
		Expires: nowSeconds + service.accessExpirySecs, IssuedAt: nowSeconds,
	})
	if err != nil {
		return authfeature.TokenPair{}, fmt.Errorf("signing access token: %w", err)
	}
	refreshToken, err := service.sign(jwtClaims{
		Subject: userID, Role: role, Kind: "refresh",
		Expires: nowSeconds + service.refreshExpirySecs, IssuedAt: nowSeconds, JTI: &refreshJTI,
	})
	if err != nil {
		return authfeature.TokenPair{}, fmt.Errorf("signing refresh token: %w", err)
	}
	return authfeature.TokenPair{
		AccessToken: accessToken, RefreshToken: refreshToken, ExpiresIn: service.accessExpirySecs,
		RefreshJTI: refreshJTI, RefreshExpiresIn: service.refreshExpirySecs,
	}, nil
}

func (service *JWTService) VerifyAccess(token string) (authfeature.AccessClaims, error) {
	return service.verify(token, "access")
}

func (service *JWTService) VerifyRefresh(token string) (authfeature.AccessClaims, error) {
	return service.verify(token, "refresh")
}

func (service *JWTService) verify(token, expectedKind string) (authfeature.AccessClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return authfeature.AccessClaims{}, fault.Unauthorized()
	}
	var header jwtHeader
	if err := decodeJWTPart(parts[0], &header); err != nil || header.Algorithm != "HS256" {
		return authfeature.AccessClaims{}, fault.Unauthorized()
	}
	providedSignature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return authfeature.AccessClaims{}, fault.Unauthorized()
	}
	mac := hmac.New(sha256.New, service.secret)
	if _, err := mac.Write([]byte(parts[0] + "." + parts[1])); err != nil {
		return authfeature.AccessClaims{}, fault.Internal(fmt.Errorf("computing token signature: %w", err))
	}
	if !hmac.Equal(providedSignature, mac.Sum(nil)) {
		return authfeature.AccessClaims{}, fault.Unauthorized()
	}
	var claims jwtClaims
	if err := decodeJWTPart(parts[1], &claims); err != nil {
		return authfeature.AccessClaims{}, fault.Unauthorized()
	}
	if claims.Kind != expectedKind || claims.Subject == "" || claims.Role == "" {
		return authfeature.AccessClaims{}, fault.Unauthorized()
	}
	if expectedKind == "refresh" && (claims.JTI == nil || *claims.JTI == "") {
		return authfeature.AccessClaims{}, fault.Unauthorized()
	}
	if expectedKind == "access" && claims.JTI != nil {
		return authfeature.AccessClaims{}, fault.Unauthorized()
	}
	now := service.now().Unix()
	if now < 0 || (uint64(now) > claims.Expires && uint64(now)-claims.Expires > uint64(jwtExpiryLeeway/time.Second)) {
		return authfeature.AccessClaims{}, fault.Unauthorized()
	}
	return authfeature.AccessClaims{UserID: claims.Subject, Role: claims.Role, JTI: claims.JTI}, nil
}

func decodeJWTPart(encoded string, target any) error {
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return err
	}
	return json.Unmarshal(decoded, target)
}

func (service *JWTService) sign(claims jwtClaims) (string, error) {
	headerBytes, err := json.Marshal(jwtHeader{Algorithm: "HS256", Type: "JWT"})
	if err != nil {
		return "", fmt.Errorf("marshaling token header: %w", err)
	}
	claimBytes, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("marshaling token claims: %w", err)
	}
	unsigned := base64.RawURLEncoding.EncodeToString(headerBytes) + "." + base64.RawURLEncoding.EncodeToString(claimBytes)
	mac := hmac.New(sha256.New, service.secret)
	if _, err := mac.Write([]byte(unsigned)); err != nil {
		return "", fmt.Errorf("computing token signature: %w", err)
	}
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

var _ authfeature.TokenService = (*JWTService)(nil)
