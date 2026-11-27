package webasset

import (
	"bytes"
	"io"
	"io/fs"
	"log"
	"net/http"
	"sync"
	"time"
)

// FileSystem wraps the public/ file system so every script and stylesheet is
// served without its comments (item 283.1). Everything else passes through
// untouched, and so does any file Strip is not sure about.
//
// Each result is kept until the file on disk changes (a different
// modification time or size), so a request costs a Stat and a map lookup -
// less than reading the file, which is what it did before. The cache only
// ever holds files someone asked for, and public/ is finite, so it is bounded
// by the size of its scripts and stylesheets less their comments.
func FileSystem(inner http.FileSystem) http.FileSystem {
	return &servedFiles{inner: inner, cache: map[string]servedFile{}}
}

type servedFiles struct {
	inner http.FileSystem
	mu    sync.Mutex
	cache map[string]servedFile
}

type servedFile struct {
	modTime time.Time
	size    int64
	body    []byte // nil: send the file as it is on disk
}

func (s *servedFiles) Open(name string) (http.File, error) {
	f, err := s.inner.Open(name)
	if err != nil || !Strippable(name) {
		return f, err
	}
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		return f, nil // http.FileServer reports the Stat error itself
	}

	s.mu.Lock()
	cached, ok := s.cache[name]
	s.mu.Unlock()
	if !ok || !cached.modTime.Equal(info.ModTime()) || cached.size != info.Size() {
		raw, err := io.ReadAll(f)
		if err != nil {
			f.Close()
			return nil, err
		}
		cached = servedFile{modTime: info.ModTime(), size: info.Size()}
		if out, err := Strip(name, raw); err == nil {
			cached.body = out
		} else {
			log.Printf("%s served with its comments: %v", name, err)
		}
		s.mu.Lock()
		s.cache[name] = cached
		s.mu.Unlock()
		if cached.body == nil {
			if _, err := f.Seek(0, io.SeekStart); err != nil {
				f.Close()
				return nil, err
			}
			return f, nil
		}
	} else if cached.body == nil {
		return f, nil
	}
	f.Close()
	return &memFile{Reader: bytes.NewReader(cached.body), info: sizedInfo{FileInfo: info, size: int64(len(cached.body))}}, nil
}

// memFile is a stripped file. http.FileServer takes the size from Stat, so
// sizedInfo reports the stripped length; the modification time stays the
// file's own, so If-Modified-Since keeps working.
type memFile struct {
	*bytes.Reader
	info fs.FileInfo
}

func (m *memFile) Close() error                         { return nil }
func (m *memFile) Readdir(int) ([]fs.FileInfo, error)   { return nil, fs.ErrInvalid }
func (m *memFile) Stat() (fs.FileInfo, error)           { return m.info, nil }

type sizedInfo struct {
	fs.FileInfo
	size int64
}

func (s sizedInfo) Size() int64 { return s.size }
