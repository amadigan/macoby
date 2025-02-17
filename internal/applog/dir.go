package applog

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/amadigan/macoby/internal/event"
	"github.com/amadigan/macoby/internal/util"
)

type Message struct {
	Subsystem string
	Data      []byte
}

type fileLog struct {
	outfile  *os.File
	stream   string
	root     string
	pattern  string
	fileLen  int64
	maxSize  int64
	maxFiles int
	files    *util.List[time.Time]
}

func (f *fileLog) run(ctx context.Context, ch <-chan []byte) {
	defer func() {
		if f.outfile != nil {
			f.outfile.Close()
		}
	}()

	for msg := range ch {
		if f.fileLen < 0 || f.fileLen+int64(len(msg)+1) > f.maxSize {
			f.rotate(ctx)
		}

		n, err := f.outfile.Write(msg)
		if err != nil {
			panic(fmt.Errorf("Failed to write to log file: %w\n", err))
		}

		if msg[len(msg)-1] != '\n' {
			n, err = f.outfile.Write([]byte{'\n'})
			if err != nil {
				panic(fmt.Errorf("Failed to write to log file: %w\n", err))
			}
		}

		f.fileLen += int64(n)
	}
}

func (f *fileLog) rotate(ctx context.Context) {
	if f.outfile != nil {
		f.outfile.Close()
	}

	ts := time.Now().UTC()
	outpath := filepath.Join(f.root, ts.Format(f.pattern))

	outfile, err := os.OpenFile(outpath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		panic(fmt.Errorf("Failed to open log file: %w\n", err))
	}

	go event.Emit(ctx, event.OpenLogFile{Path: outpath, Stream: f.stream})

	f.outfile = outfile
	f.fileLen = 0
	f.files.PushFront(ts)

	for f.files.Len() > f.maxFiles {
		ts, _ := f.files.PopBack()

		go os.Remove(filepath.Join(f.root, ts.Format(f.pattern)))
		go event.Emit(ctx, event.DeleteLogFile{Path: outpath, Stream: f.stream})
	}
}

type LogDirectory struct {
	Root        string
	NameFormat  string
	MaxFileSize int64
	MaxFiles    int
	Streams     map[string]string
	Fallback    string
}

type LogFile struct {
	Path   string
	Offset int64
}

type logNexus struct {
	streams  map[string]chan<- []byte
	fallback chan<- []byte
}

func (n *logNexus) runNexus(ch <-chan Message) {
	for event := range ch {
		if ch, ok := n.streams[event.Subsystem]; ok {
			ch <- event.Data
		} else {
			n.fallback <- event.Data
		}
	}

	for _, ch := range n.streams {
		close(ch)
	}

	close(n.fallback)
}

type logStream struct {
	name  string
	keys  []string
	files *util.List[time.Time]
}

func (d *LogDirectory) Open(ctx context.Context, ch <-chan Message) (map[string]LogFile, error) {
	files := make(map[string]*logStream, len(d.Streams)+1)

	if d.Fallback != "" {
		pattern := fmt.Sprintf(d.NameFormat, d.Fallback)
		files[pattern] = &logStream{name: d.Fallback}
	}

	files[fmt.Sprintf(d.NameFormat, d.Fallback)] = &logStream{name: d.Fallback, files: &util.List[time.Time]{}}

	for key, stream := range d.Streams {
		if stream != "" {
			pattern := fmt.Sprintf(d.NameFormat, key)
			lstream := files[pattern]

			if lstream == nil {
				lstream = &logStream{name: stream, files: &util.List[time.Time]{}}
				files[pattern] = lstream
			}

			lstream.keys = append(lstream.keys, key)
		}
	}

	if err := scanLogDirectory(d.Root, files); err != nil {
		return nil, err
	}

	nexus := &logNexus{streams: make(map[string]chan<- []byte, len(files))}

	openFiles := make(map[string]LogFile, len(files))

	for pattern, lstream := range files {
		ch, file, err := d.newFileLog(ctx, pattern, lstream.files)
		if err != nil {
			return nil, err
		}

		if file != nil {
			openFiles[lstream.name] = *file
		}

		for _, key := range lstream.keys {
			nexus.streams[key] = ch
		}

		if lstream.name == d.Fallback {
			nexus.fallback = ch
		}
	}

	go nexus.runNexus(ch)

	return openFiles, nil
}

func scanLogDirectory(root string, streams map[string]*logStream) error {
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			err = os.MkdirAll(root, 0755)
		}

		if err != nil {
			return fmt.Errorf("Failed to open log directory: %w\n", err)
		}

		return nil
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		for pattern, lstream := range streams {
			if ts, err := time.Parse(pattern, entry.Name()); err == nil {
				lstream.files.PushFront(ts)
			}
		}
	}

	return nil
}

func (d *LogDirectory) newFileLog(ctx context.Context, pattern string, files *util.List[time.Time]) (chan<- []byte, *LogFile, error) {
	fl := &fileLog{
		root:     d.Root,
		pattern:  pattern,
		maxSize:  d.MaxFileSize,
		maxFiles: d.MaxFiles,
		files:    files,
		fileLen:  -1,
	}

	var file *LogFile

	if fl.files.Len() > 0 {
		ts, _ := fl.files.Front()

		outpath := filepath.Join(fl.root, ts.Format(fl.pattern))

		outfile, err := os.OpenFile(outpath, os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return nil, nil, fmt.Errorf("Failed to open log file: %w\n", err)
		}

		fl.outfile = outfile

		pos, err := outfile.Seek(0, io.SeekEnd)
		if err != nil {
			return nil, nil, fmt.Errorf("Failed to seek to end of log file: %w\n", err)
		}

		file = &LogFile{Path: outpath, Offset: pos}
		fl.fileLen = pos
	}

	ch := make(chan []byte, 1)
	go fl.run(ctx, ch)

	return ch, file, nil
}

type MessageChanWriter struct {
	name string
	ch   chan<- Message
}

func (w *MessageChanWriter) Write(p []byte) (n int, err error) {
	w.ch <- Message{Subsystem: w.name, Data: p}

	return len(p), nil
}

func (w *MessageChanWriter) Close() error {
	close(w.ch)

	return nil
}

func NewMessageChanWriter(name string, ch chan<- Message) io.WriteCloser {
	return &MessageChanWriter{name: name, ch: ch}
}
