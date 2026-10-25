package rom

import (
	"fmt"
	"strings"

	"github.com/lcmps/retroxide/internal/shared/fault"
)

const (
	MaxPlatformLength = 64
	MaxVersionLength  = 64
	MaxRegionLength   = 64
	MaxTitleLength    = 255
)

func ValidateUpdate(title, platform string, region, version *string) error {
	if strings.TrimSpace(title) == "" || len(title) > MaxTitleLength {
		return fault.Validation("rom title is invalid")
	}
	if strings.TrimSpace(platform) == "" {
		return fault.Validation("platform cannot be empty")
	}
	return ValidateMetadata(&platform, region, version)
}

func ValidateMetadata(platform, region, version *string) error {
	if platform != nil && len(*platform) > MaxPlatformLength {
		return fault.Validation(fmt.Sprintf("platform exceeds %d characters", MaxPlatformLength))
	}
	if region != nil && len(*region) > MaxRegionLength {
		return fault.Validation(fmt.Sprintf("region exceeds %d characters", MaxRegionLength))
	}
	if version != nil && len(*version) > MaxVersionLength {
		return fault.Validation(fmt.Sprintf("version exceeds %d characters", MaxVersionLength))
	}
	return nil
}
