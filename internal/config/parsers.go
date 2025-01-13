package config

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/amadigan/macoby/internal/util"
)

func ParseSize(size string) (int64, error) {
	size = strings.ToUpper(size)

	var unit int64 = 1

	switch {
	case strings.HasSuffix(size, "T"):
		unit *= 1024

		fallthrough
	case strings.HasSuffix(size, "G"):
		unit *= 1024

		fallthrough
	case strings.HasSuffix(size, "M"):
		unit *= 1024

		fallthrough
	case strings.HasSuffix(size, "K"):
		unit *= 1024
		size = size[:len(size)-1]
	}

	count, err := strconv.ParseInt(size, 10, 64)

	if err != nil {
		return 0, fmt.Errorf("failed to parse size %s: %w", size, err)
	}

	return count * unit, nil
}

type UnmatchedVariableError struct {
	Variables []string
}

func (e *UnmatchedVariableError) Error() string {
	return fmt.Sprintf("unmatched variables: %v", e.Variables)
}

var interpRegexp = regexp.MustCompile(`\$\{[^}]+\}`)

func interpolater(input string, env map[string]string, first bool) []string {
	home := env["HOME"]

	vars := map[string]*pathVariable{}
	unmatched := map[string]struct{}{}

	interpolated := interpRegexp.ReplaceAllStringFunc(input, func(match string) string {
		name := match[2 : len(match)-1]

		eval, ok := env[name]
		if !ok {
			unmatched[name] = struct{}{}

			return match
		}

		parts := strings.Split(eval, ":")

		if len(parts) == 1 {
			return eval
		} else if first {
			return parts[0]
		}

		vars[name] = &pathVariable{values: parts}

		return match
	})

	if len(vars) == 0 {
		return []string{handleTilde(interpolated, home)}
	}

	pvars := util.MapValues(vars)

	var paths []string

	for {
		paths = append(paths, handleTilde(interpolateStep(vars, interpolated), home))

		if !increment(pvars) {
			break
		}
	}

	return paths
}

func interpolateStep(vars map[string]*pathVariable, template string) string {
	return interpRegexp.ReplaceAllStringFunc(template, func(match string) string {
		name := match[2 : len(match)-1]

		if v, ok := vars[name]; ok {
			return v.values[v.current]
		}

		return match
	})
}

func interpolateOptions(input string, env map[string]string) []string {
	return interpolater(input, env, false)
}

func interpolate(input string, env map[string]string) string {
	return interpolater(input, env, true)[0]
}

func handleTilde(path string, home string) string {
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}

	return path
}

func increment(pvars []*pathVariable) bool {
	for i := len(pvars) - 1; i >= 0; i-- {
		pvars[i].current++

		if pvars[i].current < len(pvars[i].values) {
			return true
		}

		pvars[i].current = 0
	}

	return false
}
