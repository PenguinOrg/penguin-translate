package httpapi

import (
	"io/fs"
	"mime"
	"net/http"

	rootembed "translation-overlay"
	"translation-overlay/internal/composition"
)

func init() {
	_ = mime.AddExtensionType(".mjs", "text/javascript")
	_ = mime.AddExtensionType(".js", "text/javascript")
	_ = mime.AddExtensionType(".wasm", "application/wasm")
}

func Mount(mux *http.ServeMux, app *composition.App) {
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
}

func webUIFS() fs.FS {
	sub, err := fs.Sub(rootembed.EmbeddedWebUI, "web/ui")
	if err != nil {
		panic(err)
	}
	return sub
}
