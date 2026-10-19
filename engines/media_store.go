package engines

import (
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Stage 75.4.1: the ERP server's own file store - product media, their
// thumbnails and transform cache, and document attachments - behind one
// interface, so a bucket backend (75.4.2) is one more implementation rather
// than a change at every read and write. The only implementation today is
// the local disk under mediaStoreDir, writing exactly the files, paths and
// permissions it always did.
//
// A key is a slash-separated path inside the store ("<checksum>.jpg",
// "attachments/<schema>/<random>.pdf"); it never starts with "/" and never
// climbs out with "..". A client's own image drive (Stage 95, image_drive.go)
// is a separate, per-client store and stays as it is.

// MediaStore is where the server keeps its files.
type MediaStore interface {
	// Write stores data under key. Exclusive refuses an existing key;
	// Private keeps the file and its folder unreadable to other system users.
	Write(key string, data []byte, o MediaWrite) error
	Read(key string) ([]byte, error)
	Open(key string) (io.ReadSeekCloser, error)
	Exists(key string) bool
	Remove(key string) error
}

// MediaWrite are a write's options.
type MediaWrite struct {
	Exclusive bool
	Private   bool
}

// serverMedia is the store every media and attachment read and write goes
// through.
var serverMedia MediaStore = localMediaStore{}

// validMediaKey refuses a key that could reach outside the store.
func validMediaKey(key string) error {
	if key == "" || strings.HasPrefix(key, "/") || strings.Contains(key, "\\") || strings.HasPrefix(path.Clean(key), "..") || path.Clean(key) != key {
		return fmt.Errorf("media key %q is not valid", key)
	}
	return nil
}

// localMediaStore keeps files on the server's disk under mediaStoreDir.
type localMediaStore struct{}

func (localMediaStore) path(key string) (string, error) {
	if err := validMediaKey(key); err != nil {
		return "", err
	}
	return filepath.Join(mediaStoreDir, filepath.FromSlash(key)), nil
}

func (s localMediaStore) Write(key string, data []byte, o MediaWrite) error {
	p, err := s.path(key)
	if err != nil {
		return err
	}
	dirPerm, filePerm := os.FileMode(0755), os.FileMode(0644)
	if o.Private {
		dirPerm, filePerm = 0750, 0640
	}
	if err := os.MkdirAll(filepath.Dir(p), dirPerm); err != nil {
		return fmt.Errorf("failed to prepare media storage: %v", err)
	}
	flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if o.Exclusive {
		flags = os.O_WRONLY | os.O_CREATE | os.O_EXCL
	}
	f, err := os.OpenFile(p, flags, filePerm)
	if err != nil {
		return err
	}
	_, werr := f.Write(data)
	cerr := f.Close()
	if werr != nil || cerr != nil {
		_ = os.Remove(p)
		if werr != nil {
			return werr
		}
		return cerr
	}
	return nil
}

func (s localMediaStore) Read(key string) ([]byte, error) {
	p, err := s.path(key)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(p)
}

func (s localMediaStore) Open(key string) (io.ReadSeekCloser, error) {
	p, err := s.path(key)
	if err != nil {
		return nil, err
	}
	return os.Open(p)
}

func (s localMediaStore) Exists(key string) bool {
	p, err := s.path(key)
	if err != nil {
		return false
	}
	_, err = os.Stat(p)
	return err == nil
}

func (s localMediaStore) Remove(key string) error {
	p, err := s.path(key)
	if err != nil {
		return err
	}
	return os.Remove(p)
}

// mediaKeyForStoredPath turns a ProductMedia record's file_path (written as a
// path under mediaStoreDir) into its key; "" when the path is outside the
// store.
func mediaKeyForStoredPath(stored string) string {
	rel, err := filepath.Rel(mediaStoreDir, stored)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return ""
	}
	return filepath.ToSlash(rel)
}

// The content-addressed keys of a media asset's derived files.
func thumbnailKey(checksum string) string         { return checksum + "_thumb.jpg" }
func transformKey(checksum, preset string) string { return checksum + "_transform_" + preset + ".jpg" }
