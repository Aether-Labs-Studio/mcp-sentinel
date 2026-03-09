package logx

import (
	"log"
	"os"
	"strings"
)

var debugEnabled = parseBoolEnv("SENTINEL_DEBUG")

func parseBoolEnv(key string) bool {
	value := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	switch value {
	case "1", "true", "yes", "on", "debug":
		return true
	default:
		return false
	}
}

func Debugf(format string, args ...any) {
	if debugEnabled {
		log.Printf(format, args...)
	}
}

func Enabled() bool {
	return debugEnabled
}
