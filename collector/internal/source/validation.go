package source

import (
	"fmt"
	"strings"
)

// validateEnum checks if a value is in a list of valid values
// Returns nil if valid, otherwise returns an error with a descriptive message
func validateEnum(value string, validValues []string, fieldName string) error {
	for _, v := range validValues {
		if value == v {
			return nil
		}
	}

	// Format valid values list for error message
	quotedValues := make([]string, len(validValues))
	for i, v := range validValues {
		quotedValues[i] = fmt.Sprintf("'%s'", v)
	}
	validList := strings.Join(quotedValues, ", ")

	return fmt.Errorf("invalid %s: %s (must be one of: %s)", fieldName, value, validList)
}
