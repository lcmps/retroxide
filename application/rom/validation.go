package rom

import (
	"fmt"
	"strings"

	"github.com/lcmps/retroxide/application/fault"
)

const (
	MaxPlatformLength    = 64
	MaxVersionLength     = 64
	MaxRegionLength      = 64
	MaxTitleLength       = 255
	MaxPublisherLength   = 255
	MaxReleaseYearLength = 16
	MaxDescriptionLength = 4000
)

// ValidateDetails checks the optional descriptive fields of an update.
func ValidateDetails(publisher, releaseYear, description *string) error {
	for _, field := range []struct {
		name  string
		value *string
		limit int
	}{
		{"publisher", publisher, MaxPublisherLength},
		{"release year", releaseYear, MaxReleaseYearLength},
		{"description", description, MaxDescriptionLength},
	} {
		if field.value != nil && len(strings.TrimSpace(*field.value)) > field.limit {
			return fault.Validation(fmt.Sprintf("%s exceeds %d characters", field.name, field.limit))
		}
	}
	return nil
}

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
