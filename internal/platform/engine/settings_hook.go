package engine

func whisperGPU() string {
	load := loadSettingsFn()
	if load == nil {
		return ""
	}
	st, err := load()
	if err != nil {
		return ""
	}
	return st.MicTranslate.WhisperGPU
}

func nllbGPU() string {
	load := loadSettingsFn()
	if load == nil {
		return ""
	}
	st, err := load()
	if err != nil {
		return ""
	}
	return st.MicTranslate.NLLBGPU
}
