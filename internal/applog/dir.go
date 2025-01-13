package applog

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/amadigan/macoby/internal/util"
)

type fileLog struct {
	outfile  *os.File
	root     string
	pattern  string
	fileLen  int64
	maxSize  int64
	maxFiles int
	files    *util.List[time.Time]
}

func (f *fileLog) run(ch <-chan []byte) {
	defer func() {
		if f.outfile != nil {
			f.outfile.Close()
		}
	}()

	for msg := range ch {
		if f.fileLen < 0 || f.fileLen+int64(len(msg)+1) > f.maxSize {
			f.rotate()
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

func (f *fileLog) rotate() {
	if f.outfile != nil {
		f.outfile.Close()
	}

	ts := time.Now().UTC()
	outpath := filepath.Join(f.root, ts.Format(f.pattern))

	outfile, err := os.OpenFile(outpath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		panic(fmt.Errorf("Failed to open log file: %w\n", err))
	}

	f.outfile = outfile
	f.fileLen = 0
	f.files.PushFront(ts)

	for f.files.Len() > f.maxFiles {
		ts, _ := f.files.PopBack()

		go os.Remove(filepath.Join(f.root, ts.Format(f.pattern)))
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

type logNexus struct {
	streams  map[string]chan<- []byte
	fallback chan<- []byte
}

type logStream struct {
	name  string
	keys  []string
	files *util.List[time.Time]
}

func (d LogDirectory) Open(ch <-chan Event) (bool, error) {
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

	if entries, err := os.ReadDir(d.Root); err != nil {
		if os.IsNotExist(err) {
			err = os.MkdirAll(d.Root, 0755)
		}

		if err != nil {
			return false, fmt.Errorf("Failed to open log directory: %w\n", err)
		}
	} else {
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}

			for pattern, lstream := range files {
				if ts, err := time.Parse(pattern, entry.Name()); err == nil {
					lstream.files.PushFront(ts)
				}
			}
		}
	}

	nexus := &logNexus{streams: make(map[string]chan<- []byte, len(files))}

	for pattern, lstream := range files {
		ch, err := d.newFileLog(pattern, lstream.files)
		if err != nil {
			return false, err
		}

		for _, key := range lstream.keys {
			nexus.streams[key] = ch
		}

		if lstream.name == d.Fallback {
			nexus.fallback = ch
		}
	}

	go func() {
		for event := range ch {
			if ch, ok := nexus.streams[event.Subsystem]; ok {
				ch <- event.Data
			} else {
				nexus.fallback <- event.Data
			}
		}

		for _, ch := range nexus.streams {
			close(ch)
		}

		close(nexus.fallback)
	}()

	return true, nil
}

func (d LogDirectory) newFileLog(pattern string, files *util.List[time.Time]) (chan<- []byte, error) {
	fl := &fileLog{
		root:     d.Root,
		pattern:  pattern,
		maxSize:  d.MaxFileSize,
		maxFiles: d.MaxFiles,
		files:    files,
		fileLen:  -1,
	}

	if fl.files.Len() > 0 {
		ts, _ := fl.files.Front()

		outpath := filepath.Join(fl.root, ts.Format(fl.pattern))

		outfile, err := os.OpenFile(outpath, os.O_APPEND|os.O_WRONLY, 0644)
		if err != nil {
			return nil, fmt.Errorf("Failed to open log file: %w\n", err)
		}

		fl.outfile = outfile
		pos, err := outfile.Seek(0, io.SeekEnd)
		if err != nil {
			return nil, fmt.Errorf("Failed to seek to end of log file: %w\n", err)
		}

		fl.fileLen = pos
	}

	ch := make(chan []byte, 1)
	go fl.run(ch)

	return ch, nil
}
