//go:build cgo

package compress

/*
#cgo CFLAGS: -DGOXZ_SKIP_C_COMPILATION
#cgo LDFLAGS: -llzma
*/
import "C"

import (
	"archive/zip"
	"io"

	"github.com/jamespfennell/xz"
)

// init registers the XZ method (95) with the standard library zip package
func init() {
	zip.RegisterDecompressor(XZ_METHOD, func(r io.Reader) io.ReadCloser {
		return (*xzReadCloser)(xz.NewReader(r))
	})

	zip.RegisterCompressor(XZ_METHOD, func(w io.Writer) (io.WriteCloser, error) {
		return xz.NewWriterLevel(w, xz.BestCompression), nil
	})
}

type xzReadCloser xz.Reader

func (_ *xzReadCloser) Close() error { return nil }

func (r *xzReadCloser) Read(p []byte) (n int, err error) {
	//nolint:wrapcheck
	return (*xz.Reader)(r).Read(p)
}
