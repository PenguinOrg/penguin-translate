package ocr

type Recognizer interface {
	RecognizeResult(pixels []byte, captureW, captureH int) (*Result, error)
	Close()
}
