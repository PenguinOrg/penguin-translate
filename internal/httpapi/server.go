package httpapi

import (
	"io/fs"
	"mime"
	"net/http"

	rootembed "translation-overlay"
	"translation-overlay/internal/composition"
	"translation-overlay/internal/platform/netguard"
)

func init() {
	_ = mime.AddExtensionType(".mjs", "text/javascript")
	_ = mime.AddExtensionType(".js", "text/javascript")
	_ = mime.AddExtensionType(".wasm", "application/wasm")
}

// Mount registers all routes and returns the handler both serve modes (-http
// and the Wails assetserver) must use: it wraps the mux with the browser-origin
// guard so cross-site pages cannot blind-POST the loopback API.
func Mount(mux *http.ServeMux, app *composition.App) http.Handler {
	app.MicTranslate.MountRoutes(mux)
	app.Audio.MountRoutes(mux)
	app.Window.MountRoutes(mux)

	mux.HandleFunc("/api/settings", handleSettings(app))

	mux.Handle("/ui/", http.StripPrefix("/ui/", http.FileServerFS(webUIFS())))
	mux.HandleFunc("/ui", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/ui/", http.StatusFound)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/ui/", http.StatusFound)
	})

	return netguard.RequireBrowserOriginLoopback(mux)
}

func webUIFS() fs.FS {
	sub, err := fs.Sub(rootembed.EmbeddedWebUI, "web/ui")
	if err != nil {
		panic(err)
	}
	return sub
}
