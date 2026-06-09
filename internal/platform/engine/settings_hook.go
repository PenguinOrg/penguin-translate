package engine

func whisperGPU() string {
	if settingsLoader == nil {
		return ""
	}
	st, err := settingsLoader()
	if err != nil {
		return ""
	}
	return st.MicTranslate.WhisperGPU
}

func nllbGPU() string {
	if settingsLoader == nil {
		return ""
	}
	st, err := settingsLoader()
	if err != nil {
		return ""
	}
	return st.MicTranslate.NLLBGPU
}
