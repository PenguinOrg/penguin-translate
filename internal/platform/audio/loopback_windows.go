//go:build windows

package audio

import (
	"cmp"
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/go-ole/go-ole"
	"github.com/moutend/go-wca/pkg/wca"
)

const outputRate = 16000

type loopbackCapture struct {
	mu      sync.Mutex
	stop    chan struct{}
	device  string
	label   string
	lastErr string
	subs    map[chan []byte]struct{}
}

var globalLoopback loopbackCapture

func init() {
	globalLoopback.subs = make(map[chan []byte]struct{})
}

func loopbackDeviceLabel() string {
	globalLoopback.mu.Lock()
	defer globalLoopback.mu.Unlock()
	return globalLoopback.label
}

func loopbackCaptureError() string {
	globalLoopback.mu.Lock()
	defer globalLoopback.mu.Unlock()
	return globalLoopback.lastErr
}

func loopbackCaptureRunning() bool {
	globalLoopback.mu.Lock()
	defer globalLoopback.mu.Unlock()
	return globalLoopback.stop != nil
}

func startLoopbackCapture(deviceID string) error {
	id, name, ok := lookupLoopbackDevice(deviceID)
	if !ok && strings.TrimSpace(deviceID) != "" {
		return fmt.Errorf("unknown playback device %q", deviceID)
	}
	globalLoopback.mu.Lock()
	if globalLoopback.stop != nil {
		close(globalLoopback.stop)
	}
	globalLoopback.stop = make(chan struct{})
	globalLoopback.device = id
	if name != "" {
		globalLoopback.label = name
	} else {
		globalLoopback.label = "Default output"
	}
	globalLoopback.lastErr = ""
	stop := globalLoopback.stop
	label := globalLoopback.label
	globalLoopback.mu.Unlock()

	go runLoopbackCapture(stop, id, label)
	return nil
}

func stopLoopbackCapture() {
	globalLoopback.mu.Lock()
	if globalLoopback.stop != nil {
		close(globalLoopback.stop)
		globalLoopback.stop = nil
	}
	globalLoopback.mu.Unlock()
}

func subscribeLoopback() chan []byte {
	ch := make(chan []byte, 32)
	globalLoopback.mu.Lock()
	globalLoopback.subs[ch] = struct{}{}
	globalLoopback.mu.Unlock()
	return ch
}

func unsubscribeLoopback(ch chan []byte) {
	globalLoopback.mu.Lock()
	delete(globalLoopback.subs, ch)
	globalLoopback.mu.Unlock()
}

func broadcastLoopback(chunk []byte) {
	globalLoopback.mu.Lock()
	subs := make([]chan []byte, 0, len(globalLoopback.subs))
	for ch := range globalLoopback.subs {
		subs = append(subs, ch)
	}
	globalLoopback.mu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- chunk:
		default:
		}
	}
}

func signalLoopbackEnd(errMsg string) {
	globalLoopback.mu.Lock()
	if errMsg != "" {
		globalLoopback.lastErr = errMsg
	}
	subs := make([]chan []byte, 0, len(globalLoopback.subs))
	for ch := range globalLoopback.subs {
		subs = append(subs, ch)
	}
	globalLoopback.mu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- nil:
		default:
		}
	}
}

func openRenderDevice(endpointID string) (*wca.IMMDevice, string, error) {
	ensureCOM()

	var mmde *wca.IMMDeviceEnumerator
	if err := wca.CoCreateInstance(
		wca.CLSID_MMDeviceEnumerator,
		0,
		wca.CLSCTX_ALL,
		wca.IID_IMMDeviceEnumerator,
		&mmde,
	); err != nil {
		return nil, "", fmt.Errorf("MMDeviceEnumerator: %w", err)
	}
	defer mmde.Release()

	endpointID = strings.TrimSpace(endpointID)
	if endpointID == "" {
		var mmd *wca.IMMDevice
		if err := mmde.GetDefaultAudioEndpoint(wca.ERender, wca.EConsole, &mmd); err != nil {
			return nil, "", fmt.Errorf("default render device: %w", err)
		}
		name, _ := endpointFriendlyName(mmd)
		return mmd, name, nil
	}

	var collection *wca.IMMDeviceCollection
	if err := mmde.EnumAudioEndpoints(wca.ERender, wca.DEVICE_STATE_ACTIVE, &collection); err != nil {
		return nil, "", fmt.Errorf("EnumAudioEndpoints: %w", err)
	}
	defer collection.Release()

	var count uint32
	if err := collection.GetCount(&count); err != nil {
		return nil, "", err
	}
	for i := uint32(0); i < count; i++ {
		var dev *wca.IMMDevice
		if err := collection.Item(i, &dev); err != nil {
			continue
		}
		var id string
		if err := dev.GetId(&id); err != nil {
			dev.Release()
			continue
		}
		if id != endpointID {
			dev.Release()
			continue
		}
		name, _ := endpointFriendlyName(dev)
		return dev, name, nil
	}
	return nil, "", fmt.Errorf("playback device not found for loopback (%q)", endpointID)
}

func runLoopbackCapture(stop <-chan struct{}, endpointID, endpointName string) {
	defer func() {
		globalLoopback.mu.Lock()
		if globalLoopback.stop == stop {
			globalLoopback.stop = nil
		}
		globalLoopback.mu.Unlock()
	}()

	mmd, label, err := openRenderDevice(endpointID)
	if err != nil {
		signalLoopbackEnd(err.Error())
		return
	}
	defer mmd.Release()

	label = cmp.Or(label, endpointName, "Default output")
	globalLoopback.mu.Lock()
	globalLoopback.label = label
	globalLoopback.mu.Unlock()

	var ac *wca.IAudioClient
	if err := mmd.Activate(wca.IID_IAudioClient, wca.CLSCTX_ALL, nil, &ac); err != nil {
		signalLoopbackEnd(fmt.Sprintf("IAudioClient: %v", err))
		return
	}
	defer ac.Release()

	var wfx *wca.WAVEFORMATEX
	if err := ac.GetMixFormat(&wfx); err != nil {
		signalLoopbackEnd(fmt.Sprintf("GetMixFormat: %v", err))
		return
	}
	defer ole.CoTaskMemFree(uintptr(unsafe.Pointer(wfx)))

	wfx.WFormatTag = wca.WAVE_FORMAT_PCM
	wfx.WBitsPerSample = 16
	wfx.NBlockAlign = (wfx.WBitsPerSample / 8) * wfx.NChannels
	wfx.NAvgBytesPerSec = wfx.NSamplesPerSec * uint32(wfx.NBlockAlign)
	wfx.CbSize = 0

	captureRate := int(wfx.NSamplesPerSec)
	channels := int(wfx.NChannels)
	if captureRate < 1 {
		captureRate = 48000
	}
	if channels < 1 {
		channels = 2
	}

	var defaultPeriod wca.REFERENCE_TIME
	var minimumPeriod wca.REFERENCE_TIME
	if err := ac.GetDevicePeriod(&defaultPeriod, &minimumPeriod); err != nil {
		signalLoopbackEnd(fmt.Sprintf("GetDevicePeriod: %v", err))
		return
	}
	latency := time.Duration(int(defaultPeriod) * 100)
	if latency < time.Millisecond {
		latency = 10 * time.Millisecond
	}

	if err := ac.Initialize(
		wca.AUDCLNT_SHAREMODE_SHARED,
		wca.AUDCLNT_STREAMFLAGS_LOOPBACK,
		wca.REFERENCE_TIME(400*10000),
		0,
		wfx,
		nil,
	); err != nil {
		signalLoopbackEnd(fmt.Sprintf("loopback init: %v", err))
		return
	}

	var acc *wca.IAudioCaptureClient
	if err := ac.GetService(wca.IID_IAudioCaptureClient, &acc); err != nil {
		signalLoopbackEnd(fmt.Sprintf("IAudioCaptureClient: %v", err))
		return
	}
	defer acc.Release()

	if err := ac.Start(); err != nil {
		signalLoopbackEnd(fmt.Sprintf("loopback start: %v", err))
		return
	}
	defer func() { _ = ac.Stop() }()

	globalLoopback.mu.Lock()
	globalLoopback.lastErr = ""
	globalLoopback.mu.Unlock()

	time.Sleep(latency)

	blockAlign := int(wfx.NBlockAlign)
	if blockAlign < 1 {
		blockAlign = 4
	}

	for {
		select {
		case <-stop:
			signalLoopbackEnd("")
			return
		default:
		}

		var (
			data               *byte
			availableFrameSize uint32
			flags              uint32
			devicePosition     uint64
			qpcPosition        uint64
		)
		if err := acc.GetBuffer(&data, &availableFrameSize, &flags, &devicePosition, &qpcPosition); err != nil {
			time.Sleep(latency / 2)
			continue
		}
		if availableFrameSize == 0 {
			_ = acc.ReleaseBuffer(0)
			time.Sleep(latency / 2)
			continue
		}

		lim := int(availableFrameSize) * blockAlign
		raw := make([]byte, lim)
		if flags&wca.AUDCLNT_BUFFERFLAGS_SILENT == 0 && data != nil {
			start := unsafe.Pointer(data)
			for n := 0; n < lim; n++ {
				b := (*byte)(unsafe.Pointer(uintptr(start) + uintptr(n)))
				raw[n] = *b
			}
		}

		if err := acc.ReleaseBuffer(availableFrameSize); err != nil {
			signalLoopbackEnd(fmt.Sprintf("ReleaseBuffer: %v", err))
			return
		}

		chunk := pcmTo16kMono(raw, captureRate, channels, int(wfx.WBitsPerSample))
		if len(chunk) > 0 {
			broadcastLoopback(chunk)
		}
		time.Sleep(latency / 2)
	}
}

func pcmTo16kMono(data []byte, sampleRate, channels, bitsPerSample int) []byte {
	if len(data) < 2 || channels < 1 {
		return nil
	}
	bytesPerSample := bitsPerSample / 8
	if bytesPerSample < 2 {
		bytesPerSample = 2
	}
	frameBytes := bytesPerSample * channels
	if frameBytes < 1 {
		return nil
	}
	nFrames := len(data) / frameBytes
	if nFrames < 1 {
		return nil
	}

	samples := make([]float64, nFrames)
	switch bytesPerSample {
	case 2:
		for i := 0; i < nFrames; i++ {
			off := i * frameBytes
			var sum float64
			for c := 0; c < channels; c++ {
				v := int16(binary.LittleEndian.Uint16(data[off+c*2 : off+c*2+2]))
				sum += float64(v) / 32768.0
			}
			samples[i] = sum / float64(channels)
		}
	case 4:
		for i := 0; i < nFrames; i++ {
			off := i * frameBytes
			var sum float64
			for c := 0; c < channels; c++ {
				bits := binary.LittleEndian.Uint32(data[off+c*4 : off+c*4+4])
				sum += float32FromBits(bits)
			}
			samples[i] = sum / float64(channels)
		}
	default:
		return nil
	}

	if sampleRate != outputRate {
		nOut := int(float64(len(samples)) * float64(outputRate) / float64(sampleRate))
		if nOut < 1 {
			nOut = 1
		}
		out := make([]float64, nOut)
		denom := float64(nOut - 1)
		if denom < 1 {
			denom = 1
		}
		for i := 0; i < nOut; i++ {
			src := float64(i) * float64(len(samples)-1) / denom
			j := int(src)
			if j >= len(samples)-1 {
				out[i] = samples[len(samples)-1]
			} else {
				frac := src - float64(j)
				out[i] = samples[j]*(1-frac) + samples[j+1]*frac
			}
		}
		samples = out
	}

	pcm := make([]byte, len(samples)*2)
	for i, s := range samples {
		s = math.Max(-1, math.Min(1, s))
		v := int16(s * 32767)
		pcm[i*2] = byte(v)
		pcm[i*2+1] = byte(v >> 8)
	}
	return pcm
}

func float32FromBits(bits uint32) float64 {
	return float64(math.Float32frombits(bits))
}
