package utils

import (
	"path"
	"strings"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
)

var matchPatternLogger = logging.NewSubysLogger("utils")

// MatchAnyPattern checks if value matches any glob pattern in patterns.
func MatchAnyPattern(patterns []string, value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return false
	}

	for _, pattern := range patterns {
		pat := strings.ToLower(strings.TrimSpace(pattern))
		if pat == "" {
			continue
		}
		if pat == "*" {
			return true
		}
		matched, err := path.Match(pat, normalized)
		if err != nil {
			matchPatternLogger.WithError(err).Warnf("invalid match pattern %q", pattern)
			continue
		}
		if matched {
			return true
		}
	}
	return false
}
