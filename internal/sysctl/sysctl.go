package sysctl

import (
	"fmt"
	"io/fs"
	"strings"
)

func LoadSysctls(root fs.FS, files ...string) (map[string]string, error) {
	ctls := map[string]string{}

	for _, file := range files {
		data, err := fs.ReadFile(root, file)
		if err != nil {
			return nil, fmt.Errorf("failed to read %s: %w", file, err)
		}

		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
				continue
			}

			parts := strings.SplitN(line, "=", 2)
			if len(parts) != 2 {
				return nil, fmt.Errorf("invalid line: %s", line)
			}

			ctls[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}

	return ctls, nil
}
