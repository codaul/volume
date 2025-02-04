package settings

import (
	"os"
	"strconv"
	"time"
)

//Env returns a volume environment variable
func Env(name string) string {
	return os.Getenv("volume_" + name)
}

//EnvInt returns an integer using an environment variable, with a default fallback
func EnvInt(name string, def int) int {
	if n, err := strconv.Atoi(Env(name)); err == nil {
		return n
	}
	return def
}

//EnvDuration returns a duration using an environment variable, with a default fallback
func EnvDuration(name string, def time.Duration) time.Duration {
	if n, err := time.ParseDuration(Env(name)); err == nil {
		return n
	}
	return def
}
