package audio

type LoopbackDevice struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	IsDefault  bool   `json:"is_default"`
	LoopbackOK bool   `json:"loopback_ok"`
}

func ListLoopbackDevices() ([]LoopbackDevice, error) { return listLoopbackDevices() }

func LoopbackDeviceLabel() string { return loopbackDeviceLabel() }

func LoopbackCaptureError() string { return loopbackCaptureError() }

func LoopbackCaptureRunning() bool { return loopbackCaptureRunning() }

func StartLoopbackCapture(deviceID string) error { return startLoopbackCapture(deviceID) }

func StopLoopbackCapture() { stopLoopbackCapture() }

func SubscribeLoopback() chan []byte { return subscribeLoopback() }

func UnsubscribeLoopback(ch chan []byte) { unsubscribeLoopback(ch) }
