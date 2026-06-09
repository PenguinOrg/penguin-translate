package applog

import (
	"fmt"
	"os"
	"sync"
)

type RotatingWriter struct {
	mu       sync.Mutex
	path     string
	maxSize  int64
	maxFiles int
	f        *os.File
	size     int64
}

func NewRotatingWriter(path string, maxSize int64, maxFiles int) (*RotatingWriter, error) {
	w := &RotatingWriter{path: path, maxSize: maxSize, maxFiles: maxFiles}
	if err := w.open(); err != nil {
		return nil, err
	}
	if w.size >= w.maxSize {
		if err := w.rotate(); err != nil {
			return nil, err
		}
	}
	return w, nil
}

func (w *RotatingWriter) open() error {
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return err
	}
	w.f, w.size = f, info.Size()
	return nil
}

func (w *RotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.size+int64(len(p)) > w.maxSize {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := w.f.Write(p)
	w.size += int64(n)
	return n, err
}

func (w *RotatingWriter) rotate() error {
	if w.f != nil {
		_ = w.f.Close()
		w.f = nil
	}
	for i := w.maxFiles; i >= 1; i-- {
		src := w.path
		if i > 1 {
			src = fmt.Sprintf("%s.%d", w.path, i-1)
		}
		dst := fmt.Sprintf("%s.%d", w.path, i)
		_ = os.Remove(dst)
		_ = os.Rename(src, dst)
	}
	return w.open()
}

func (w *RotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return nil
	}
	err := w.f.Close()
	w.f = nil
	return err
}
