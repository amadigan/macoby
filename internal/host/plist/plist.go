package plist

import (
	"encoding/xml"
	"fmt"

	"github.com/amadigan/macoby/internal/util"
)

type PropertyList map[string]any
type Dict map[string]any
type String string
type Integer int64
type Boolean bool
type Array []any

func (p PropertyList) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	el := xml.StartElement{Name: xml.Name{Local: "plist"}, Attr: []xml.Attr{{Name: xml.Name{Local: "version"}, Value: "1.0"}}}

	if err := e.EncodeToken(el); err != nil {
		return fmt.Errorf("failed to encode plist start element: %w", err)
	}

	if err := e.Encode(Dict(p)); err != nil {
		return fmt.Errorf("failed to encode plist dict: %w", err)
	}

	if err := e.EncodeToken(el.End()); err != nil {
		return fmt.Errorf("failed to encode plist end element: %w", err)
	}

	return nil
}

func (d Dict) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	el := xml.StartElement{Name: xml.Name{Local: "dict"}}
	if err := e.EncodeToken(el); err != nil {
		return fmt.Errorf("failed to encode dict start element: %w", err)
	}

	for _, key := range util.SortKeys(d) {
		if err := e.EncodeElement(xml.CharData(key), xml.StartElement{Name: xml.Name{Local: "key"}}); err != nil {
			return fmt.Errorf("failed to encode dict key: %w", err)
		}

		if err := e.Encode(d[key]); err != nil {
			return fmt.Errorf("failed to encode dict value: %w", err)
		}
	}

	if err := e.EncodeToken(el.End()); err != nil {
		return fmt.Errorf("failed to encode dict end element: %w", err)
	}

	return nil
}

func (s String) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	el := xml.StartElement{Name: xml.Name{Local: "string"}}
	if err := e.EncodeElement(xml.CharData(s), el); err != nil {
		return fmt.Errorf("failed to encode string: %w", err)
	}

	return nil
}

func (a Array) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	el := xml.StartElement{Name: xml.Name{Local: "array"}}

	if err := e.EncodeToken(el); err != nil {
		return fmt.Errorf("failed to encode array start element: %w", err)
	}

	for _, item := range a {
		if err := e.Encode(item); err != nil {
			return fmt.Errorf("failed to encode array item: %w", err)
		}
	}

	if err := e.EncodeToken(el.End()); err != nil {
		return fmt.Errorf("failed to encode array end element: %w", err)
	}

	return nil
}

func (i Integer) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	el := xml.StartElement{Name: xml.Name{Local: "integer"}}
	if err := e.EncodeElement(xml.CharData(fmt.Sprintf("%d", i)), el); err != nil {
		return fmt.Errorf("failed to encode integer: %w", err)
	}

	return nil
}

func (b Boolean) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	el := xml.StartElement{Name: xml.Name{Local: "true"}}
	if !b {
		el.Name.Local = "false"
	}

	if err := e.EncodeToken(el); err != nil {
		return fmt.Errorf("failed to encode boolean: %w", err)
	}

	return nil
}
