package interfaces

import (
	"io"
	"os"
)

// FileObject provides common interface for FS file objects
type FileObject interface {
	io.ReadWriteSeeker
	io.Closer
	Stat() (os.FileInfo, error)
	Name() string
}
