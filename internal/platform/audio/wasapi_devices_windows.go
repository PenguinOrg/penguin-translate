//go:build windows

package audio

import (
	"fmt"
	"strings"
	"sync"

	"github.com/go-ole/go-ole"
	"github.com/moutend/go-wca/pkg/wca"
)

var comInitOnce sync.Once

func ensureCOM() {
	comInitOnce.Do(func() {
		_ = ole.CoInitializeEx(0, ole.COINIT_MULTITHREADED)
	})
}

func listLoopbackDevices() ([]LoopbackDevice, error) {
	ensureCOM()

	var mmde *wca.IMMDeviceEnumerator
	if err := wca.CoCreateInstance(
		wca.CLSID_MMDeviceEnumerator,
		0,
		wca.CLSCTX_ALL,
		wca.IID_IMMDeviceEnumerator,
		&mmde,
	); err != nil {
		return nil, fmt.Errorf("MMDeviceEnumerator: %w", err)
	}
	defer mmde.Release()

	defaultID := ""
	var defaultDev *wca.IMMDevice
	if err := mmde.GetDefaultAudioEndpoint(wca.ERender, wca.DEVICE_STATE_ACTIVE, &defaultDev); err == nil && defaultDev != nil {
		_ = defaultDev.GetId(&defaultID)
		defaultDev.Release()
	}

	var collection *wca.IMMDeviceCollection
	if err := mmde.EnumAudioEndpoints(wca.ERender, wca.DEVICE_STATE_ACTIVE, &collection); err != nil {
		return nil, fmt.Errorf("EnumAudioEndpoints: %w", err)
	}
	defer collection.Release()

	var count uint32
	if err := collection.GetCount(&count); err != nil {
		return nil, fmt.Errorf("GetCount: %w", err)
	}

	out := make([]LoopbackDevice, 0, count)
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
		name, err := endpointFriendlyName(dev)
		dev.Release()
		if err != nil || strings.TrimSpace(name) == "" {
			name = id
		}
		out = append(out, LoopbackDevice{
			ID:         id,
			Name:       name,
			IsDefault:  id == defaultID,
			LoopbackOK: true,
		})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no active playback devices")
	}
	return out, nil
}

func endpointFriendlyName(mmd *wca.IMMDevice) (string, error) {
	var ps *wca.IPropertyStore
	if err := mmd.OpenPropertyStore(wca.STGM_READ, &ps); err != nil {
		return "", err
	}
	defer ps.Release()

	var pv wca.PROPVARIANT
	if err := ps.GetValue(&wca.PKEY_Device_FriendlyName, &pv); err != nil {
		return "", err
	}
	return pv.String(), nil
}

func listOutputDevices() []OutputDevice {
	devs, err := listLoopbackDevices()
	if err != nil || len(devs) == 0 {
		return []OutputDevice{{ID: "default", Name: "System default", IsDefault: true}}
	}
	out := make([]OutputDevice, len(devs))
	for i, d := range devs {
		out[i] = OutputDevice{ID: d.ID, Name: d.Name, IsDefault: d.IsDefault}
	}
	return out
}

func lookupLoopbackDevice(deviceID string) (id, name string, ok bool) {
	deviceID = strings.TrimSpace(deviceID)
	devs, err := listLoopbackDevices()
	if err != nil {
		return "", "", false
	}
	if deviceID == "" {
		for _, d := range devs {
			if d.IsDefault {
				return d.ID, d.Name, true
			}
		}
		if len(devs) > 0 {
			return devs[0].ID, devs[0].Name, true
		}
		return "", "", false
	}
	for _, d := range devs {
		if d.ID == deviceID || d.Name == deviceID {
			return d.ID, d.Name, true
		}
	}
	return "", "", false
}
