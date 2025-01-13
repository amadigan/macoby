package util

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/tidwall/jsonc"
)

func ReadJsonConfig(abspath string, v any) error {
	bs, err := os.ReadFile(abspath)

	if err != nil {
		return fmt.Errorf("failed to read file %s: %w", abspath, err)
	}

	bs = jsonc.ToJSONInPlace(bs)

	if err := json.Unmarshal(bs, v); err != nil {
		return fmt.Errorf("error reading %s: failed to parse json: %w", abspath, err)
	}

	return nil
}
