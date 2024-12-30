package util

import "io"

type CountingWriter struct {
	writer io.Writer
	count  int64
}

func NewCountingWriter(writer io.Writer) *CountingWriter {
	return &CountingWriter{writer: writer}
}

func (cw *CountingWriter) Write(p []byte) (int, error) {
	n, err := cw.writer.Write(p)
	cw.count += int64(n)
	return n, err
}

func (cw *CountingWriter) Count() int64 {
	return cw.count
}

func (cw *CountingWriter) Close() error {
	return nil
}
