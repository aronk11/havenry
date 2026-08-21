package controlplane

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed web/*
var webFS embed.FS

// webUI liefert die eingebettete Oberfläche aus (ADR-0017).
//
// Ein Binary, kein Frontend-Deployment, keine Node-Laufzeit. Die Oberfläche
// nutzt ausschließlich /api/v1 und hat keine Sonderrechte (ADR-0009) — sie ist
// damit austauschbar, und ein CLI- oder Mobile-Client sieht exakt dieselben
// Daten.
func webUI() http.Handler {
	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		// Kann nur bei einem Baufehler passieren; dann ist das Binary kaputt.
		panic("eingebettete oberfläche fehlt: " + err.Error())
	}
	return http.FileServer(http.FS(sub))
}
