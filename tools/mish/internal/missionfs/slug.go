// Package missionfs owns the on-disk mission directory format.
package missionfs

import (
	"fmt"
	"regexp"
	"strings"
)

const maxSlugLength = 64

var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// ValidateSlug verifies the spec's mission slug rules.
func ValidateSlug(slug string) error {
	switch {
	case slug == "":
		return fmt.Errorf("slug is required")
	case len(slug) > maxSlugLength:
		return fmt.Errorf("slug must be at most %d characters", maxSlugLength)
	case strings.Contains(slug, "--"):
		return fmt.Errorf("slug must not contain consecutive hyphens")
	case strings.HasSuffix(slug, "-"):
		return fmt.Errorf("slug must not end with a hyphen")
	case !slugPattern.MatchString(slug):
		return fmt.Errorf("slug must start with a lowercase letter or digit and contain only lowercase letters, digits, and hyphens")
	default:
		return nil
	}
}
