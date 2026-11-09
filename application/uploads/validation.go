package uploads

import (
	"fmt"
	"strings"

	"github.com/lcmps/retroxide/application/fault"
)

const MaxFilenameLength = 255

func ValidateFilename(filename string) error {
	if filename == "" {
		return fault.Validation("filename cannot be empty")
	}
	if len(filename) > MaxFilenameLength {
		return fault.Validation(fmt.Sprintf("filename exceeds %d characters", MaxFilenameLength))
	}
	if filename == "." || filename == ".." || strings.ContainsAny(filename, "/\\\x00") {
		return fault.Validation("filename contains invalid characters")
	}
	return nil
}
