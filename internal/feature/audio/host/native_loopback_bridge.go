package host

import (
	audiosys "translation-overlay/internal/platform/audio"
)

func nativeLoopbackDevices() ([]audiosys.LoopbackDevice, error) {
	return audiosys.ListLoopbackDevices()
}

func nativeLoopbackLabel() string {
	return audiosys.LoopbackDeviceLabel()
}

func nativeLoopbackCaptureError() string {
	return audiosys.LoopbackCaptureError()
}

func nativeLoopbackCaptureRunning() bool {
	return audiosys.LoopbackCaptureRunning()
}

func nativeStartLoopbackCapture(deviceID string) error {
	return audiosys.StartLoopbackCapture(deviceID)
}

func nativeStopLoopbackCapture() {
	audiosys.StopLoopbackCapture()
}

func nativeSubscribeLoopback() chan []byte {
	return audiosys.SubscribeLoopback()
}

func nativeUnsubscribeLoopback(ch chan []byte) {
	audiosys.UnsubscribeLoopback(ch)
}
