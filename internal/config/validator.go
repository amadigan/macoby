package config

import (
	"encoding/json"
	"reflect"
	"strings"
)

type UnknownFieldError struct {
	Field string
	Type  string
}

func (e *UnknownFieldError) Error() string {
	return "unknown field \"" + e.Field + "\" for type " + e.Type
}

type fieldValidator struct {
	fields map[string]struct{}
	typ    string
}

func (f *fieldValidator) Validate(bs []byte) error {
	var m map[string]any

	if err := json.Unmarshal(bs, &m); err != nil {
		return err
	}

	for k := range m {
		if _, ok := f.fields[k]; !ok {
			return &UnknownFieldError{Field: k, Type: f.typ}
		}
	}

	return nil
}

func newFieldValidator(example any) *fieldValidator {
	// example is a struct value, iterate over its fields
	t := reflect.TypeOf(example)
	fields := make(map[string]struct{}, t.NumField())

	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}

		name := f.Name

		// read the json tag
		tag := f.Tag.Get("json")
		parts := strings.SplitN(tag, ",", 2)

		if parts[0] == "-" {
			continue
		}

		if parts[0] != "" {
			name = parts[0]
		}

		name = strings.ToLower(name)
		fields[name] = struct{}{}
	}

	return &fieldValidator{fields: fields, typ: t.Name()}
}
