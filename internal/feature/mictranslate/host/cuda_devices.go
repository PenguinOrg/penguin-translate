package host

import (
	"encoding/json"
	"net/http"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"translation-overlay/internal/platform/osproc"
)

type cudaDeviceRow struct {
	ID             string `json:"id"`
	Label          string `json:"label"`
	MemoryTotalMiB int    `json:"memory_total_mib,omitempty"`
}

var gpuShortRE = regexp.MustCompile(`(?i)(RTX\s*\d+\s*(?:Laptop\s*)?(?:GPU)?|GTX\s*\d+)`)

func friendlyGPULabel(fullName string, memMiB int) string {
	fullName = strings.TrimSpace(fullName)
	short := fullName
	if m := gpuShortRE.FindString(fullName); m != "" {
		short = strings.TrimSpace(strings.ReplaceAll(m, "  ", " "))
	}
	if memMiB > 0 {
		return short + " (" + strconv.Itoa(memMiB) + " MiB)"
	}
	return short
}

func listCudaDevices() []cudaDeviceRow {
	if runtime.GOOS != "windows" && runtime.GOOS != "linux" {
		return nil
	}
	cmd := exec.Command(
		"nvidia-smi",
		"--query-gpu=index,name,memory.total",
		"--format=csv,noheader,nounits",
	)
	osproc.Hide(cmd)
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var rows []cudaDeviceRow
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.Split(line, ",")
		if len(parts) < 2 {
			continue
		}
		name := strings.TrimSpace(parts[1])
		if name == "" {
			continue
		}
		mem := 0
		if len(parts) >= 3 {
			mem, _ = strconv.Atoi(strings.TrimSpace(parts[2]))
		}
		rows = append(rows, cudaDeviceRow{
			ID:             name,
			Label:          friendlyGPULabel(name, mem),
			MemoryTotalMiB: mem,
		})
	}
	return rows
}

func handleCudaDevices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	devs := listCudaDevices()
	if devs == nil {
		devs = []cudaDeviceRow{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"devices": devs})
}
