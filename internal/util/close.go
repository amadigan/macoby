package util

import "io"

type closeWriter struct {
	w io.Writer
}

func NewCloseWriter(w io.Writer) io.WriteCloser {
	return &closeWriter{w: w}
}

func (cw *closeWriter) Write(p []byte) (int, error) {
	return cw.w.Write(p)
}

func (cw *closeWriter) Close() error {
	return nil
}
