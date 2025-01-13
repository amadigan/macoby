package plist

import (
	"encoding/xml"

	"github.com/amadigan/macoby/internal/util"
)

type PropertyList map[string]any
type Dict map[string]any
type String string
type Array []any

func (p PropertyList) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	el := xml.StartElement{Name: xml.Name{Local: "plist"}, Attr: []xml.Attr{{Name: xml.Name{Local: "version"}, Value: "1.0"}}}

	if err := e.EncodeToken(el); err != nil {
		return err
	}

	if err := e.Encode(Dict(p)); err != nil {
		return err
	}

	return e.EncodeToken(el.End())
}

func (d Dict) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	el := xml.StartElement{Name: xml.Name{Local: "dict"}}
	if err := e.EncodeToken(el); err != nil {
		return err
	}

	for _, key := range util.SortKeys(d) {
		if err := e.EncodeElement(xml.CharData(key), xml.StartElement{Name: xml.Name{Local: "key"}}); err != nil {
			return err
		}

		if err := e.Encode(d[key]); err != nil {
			return err
		}
	}

	return e.EncodeToken(el.End())
}

func (s String) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	el := xml.StartElement{Name: xml.Name{Local: "string"}}
	return e.EncodeElement(xml.CharData(s), el)
}

func (a Array) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	el := xml.StartElement{Name: xml.Name{Local: "array"}}

	if err := e.EncodeToken(el); err != nil {
		return err
	}

	for _, item := range a {
		if err := e.Encode(item); err != nil {
			return err
		}
	}

	return e.EncodeToken(el.End())
}
