package auth

import (
	"strings"
	"testing"
	"time"
)

func TestJWTServiceIssuesCompatibleAccessAndRefreshTokens(t *testing.T) {
	service := NewJWTService("01234567890123456789012345678901", 900, 604800)
	now := time.Unix(1_700_000_000, 0).UTC()
	service.now = func() time.Time { return now }

	pair, err := service.Issue("user-1", "admin", 7)
	if err != nil {
		t.Fatal(err)
	}
	access := strings.Split(pair.AccessToken, ".")
	refresh := strings.Split(pair.RefreshToken, ".")
	if len(access) != 3 || len(refresh) != 3 {
		t.Fatalf("token segments access=%d refresh=%d", len(access), len(refresh))
	}
	var accessClaims map[string]any
	if err := decodeJWTPart(access[1], &accessClaims); err != nil {
		t.Fatal(err)
	}
	if accessClaims["sub"] != "user-1" || accessClaims["role"] != "admin" || accessClaims["kind"] != "access" {
		t.Fatalf("access claims = %#v", accessClaims)
	}
	if _, exists := accessClaims["jti"]; exists {
		t.Fatalf("access token unexpectedly has jti: %#v", accessClaims)
	}
	var refreshClaims map[string]any
	if err := decodeJWTPart(refresh[1], &refreshClaims); err != nil {
		t.Fatal(err)
	}
	if refreshClaims["kind"] != "refresh" || refreshClaims["jti"] != pair.RefreshJTI {
		t.Fatalf("refresh claims = %#v pair=%+v", refreshClaims, pair)
	}
	verifiedAccess, err := service.VerifyAccess(pair.AccessToken)
	if err != nil || verifiedAccess.UserID != "user-1" || verifiedAccess.TokenVersion != 7 || verifiedAccess.JTI != nil {
		t.Fatalf("VerifyAccess() = %+v, %v", verifiedAccess, err)
	}
	verifiedRefresh, err := service.VerifyRefresh(pair.RefreshToken)
	if err != nil || verifiedRefresh.TokenVersion != 7 || verifiedRefresh.JTI == nil || *verifiedRefresh.JTI != pair.RefreshJTI {
		t.Fatalf("VerifyRefresh() = %+v, %v", verifiedRefresh, err)
	}
}

func TestJWTServiceRejectsWrongKindTamperingAndExpiredBeyondLeeway(t *testing.T) {
	service := NewJWTService("01234567890123456789012345678901", 10, 20)
	now := time.Unix(1_700_000_000, 0).UTC()
	service.now = func() time.Time { return now }
	pair, err := service.Issue("user-1", "user", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.VerifyRefresh(pair.AccessToken); err == nil {
		t.Fatal("VerifyRefresh(access token) succeeded")
	}
	segments := strings.Split(pair.AccessToken, ".")
	first := byte('A')
	if segments[2][0] == first {
		first = 'B'
	}
	tampered := segments[0] + "." + segments[1] + "." + string(first) + segments[2][1:]
	if _, err := service.VerifyAccess(tampered); err == nil {
		t.Fatal("VerifyAccess(tampered token) succeeded")
	}
	service.now = func() time.Time { return now.Add(71 * time.Second) }
	if _, err := service.VerifyAccess(pair.AccessToken); err == nil {
		t.Fatal("VerifyAccess(expired token) succeeded beyond 60-second leeway")
	}
}
