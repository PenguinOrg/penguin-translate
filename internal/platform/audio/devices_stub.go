//go:build !windows

package audio

func listOutputDevices() []OutputDevice {
	return []OutputDevice{{ID: "default", Name: "System default", IsDefault: true}}
}
