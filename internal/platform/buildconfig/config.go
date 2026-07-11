package buildconfig

import "strings"

// PenguinBaseURL is populated in release builds with:
//
//	-ldflags "-X translation-overlay/internal/platform/buildconfig.PenguinBaseURL=https://..."
//
// It intentionally has no source-code default.
var PenguinBaseURL string

func PenguinBase() string {
	return strings.TrimRight(strings.TrimSpace(PenguinBaseURL), "/")
}
