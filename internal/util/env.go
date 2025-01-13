package util

import (
	"os"
	"strings"
)

func Env() map[string]string {
	environ := os.Environ()

	env := make(map[string]string, len(environ))

	for _, pair := range environ {
		parts := strings.SplitN(pair, "=", 2)

		env[parts[0]] = parts[1]
	}

	return env
}
