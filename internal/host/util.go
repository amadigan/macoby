package host

import (
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"slices"

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

func sliceOf[T any](v ...T) []T {
	return v
}

func sortKeys[K cmp.Ordered, V any](m map[K]V) []K {
	keys := make([]K, 0, len(m))

	for k := range m {
		keys = append(keys, k)
	}

	slices.Sort(keys)

	return keys
}
