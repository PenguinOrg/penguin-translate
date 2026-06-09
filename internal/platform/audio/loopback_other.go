//go:build !windows

package audio

func listLoopbackDevices() ([]LoopbackDevice, error) {
	return nil, nil
}

func loopbackDeviceLabel() string { return "Output" }

func loopbackCaptureError() string { return "loopback capture is only supported on Windows" }

func loopbackCaptureRunning() bool { return false }

func startLoopbackCapture(string) error {
	return errLoopbackUnsupported
}

func stopLoopbackCapture() {}

func subscribeLoopback() chan []byte { return make(chan []byte) }

func unsubscribeLoopback(chan []byte) {}

var errLoopbackUnsupported = &loopbackErr{"loopback capture is only supported on Windows"}

type loopbackErr struct{ msg string }

func (e *loopbackErr) Error() string { return e.msg }
