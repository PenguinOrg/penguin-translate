//go:build !windows

package host

import "net/http"

func (h *Host) MountRoutes(_ *http.ServeMux) {}
