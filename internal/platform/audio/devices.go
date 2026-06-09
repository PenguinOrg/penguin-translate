package audio

type OutputDevice struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	IsDefault bool   `json:"is_default,omitempty"`
}

func ListOutputDevices() []OutputDevice {
	return listOutputDevices()
}
