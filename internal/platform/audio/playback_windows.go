//go:build windows

package audio

import (
	"encoding/binary"
	"fmt"
	"os"
	"sync"
	"syscall"
	"unsafe"
)

var (
	winmm          = syscall.NewLazyDLL("winmm.dll")
	procPlaySoundW = winmm.NewProc("PlaySoundW")
	playMu         sync.Mutex
)

const (
	sndAsync    = 0x0001
	sndMemory   = 0x0004
	sndFilename = 0x00020000
)

func PlayWAV(path string) error {
	pathW, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	playMu.Lock()
	defer playMu.Unlock()
	r, _, e := procPlaySoundW.Call(
		uintptr(unsafe.Pointer(pathW)),
		0,
		uintptr(sndAsync|sndFilename),
	)
	if r == 0 {
		if e != syscall.Errno(0) {
			return e
		}
		return fmt.Errorf("PlaySound failed")
	}
	return nil
}

func PlayPCM16LE(samples []byte, sampleRate int) error {
	if len(samples) == 0 {
		return nil
	}
	if sampleRate <= 0 {
		sampleRate = 24000
	}
	f, err := os.CreateTemp("", "to-tts-*.wav")
	if err != nil {
		return err
	}
	path := f.Name()
	defer os.Remove(path)
	if err := writePCM16WAV(f, samples, sampleRate, 1); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return PlayWAV(path)
}

func writePCM16WAV(w *os.File, pcm []byte, sampleRate, channels int) error {
	dataSize := uint32(len(pcm))
	byteRate := uint32(sampleRate * channels * 2)
	blockAlign := uint16(channels * 2)
	hdr := make([]byte, 44)
	copy(hdr[0:4], "RIFF")
	binary.LittleEndian.PutUint32(hdr[4:8], 36+dataSize)
	copy(hdr[8:12], "WAVE")
	copy(hdr[12:16], "fmt ")
	binary.LittleEndian.PutUint32(hdr[16:20], 16)
	binary.LittleEndian.PutUint16(hdr[20:22], 1)
	binary.LittleEndian.PutUint16(hdr[22:24], uint16(channels))
	binary.LittleEndian.PutUint32(hdr[24:28], uint32(sampleRate))
	binary.LittleEndian.PutUint32(hdr[28:32], byteRate)
	binary.LittleEndian.PutUint16(hdr[32:34], blockAlign)
	binary.LittleEndian.PutUint16(hdr[34:36], 16)
	copy(hdr[36:40], "data")
	binary.LittleEndian.PutUint32(hdr[40:44], dataSize)
	if _, err := w.Write(hdr); err != nil {
		return err
	}
	_, err := w.Write(pcm)
	return err
}
