package translationoverlay

import "embed"

//go:embed runtime/inference
var EmbeddedInference embed.FS

//go:embed web/ui
var EmbeddedWebUI embed.FS
