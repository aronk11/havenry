package controlplane

import (
	"net/http"
	"strings"
)

// CORS für getrennt ausgelieferte Oberflächen (ADR-0032).
//
// Die mitgelieferte Web-UI kommt aus demselben Binary und braucht das nicht.
// Eine eigenständig ausgelieferte Oberfläche — etwa die Console — läuft unter
// einer anderen Herkunft und kann die API sonst nicht ansprechen.
//
// Die Liste erlaubter Herkünfte ist standardmäßig **leer**. Eine offene
// CORS-Regel wäre ein Loch, kein Feature: Bei einem Werkzeug mit Sitzungs-
// Cookies und Root-Rechten auf allen Hosts würde sie jeder Webseite erlauben,
// im Namen eines angemeldeten Nutzers zu handeln.

// corsConfig hält die erlaubten Herkünfte.
type corsConfig struct {
	origins map[string]bool
}

func newCORS(origins []string) *corsConfig {
	c := &corsConfig{origins: map[string]bool{}}
	for _, o := range origins {
		o = strings.TrimRight(strings.TrimSpace(o), "/")
		if o != "" {
			c.origins[o] = true
		}
	}
	return c
}

// allowed prüft eine Herkunft.
//
// Exakter Vergleich, keine Platzhalter. Muster wie "*.example.com" sind eine
// bekannte Quelle für Fehler — ein Tippfehler im Muster fällt erst auf, wenn
// jemand die Lücke nutzt.
func (c *corsConfig) allowed(origin string) bool {
	if origin == "" || len(c.origins) == 0 {
		return false
	}
	return c.origins[strings.TrimRight(origin, "/")]
}

// enabled meldet, ob überhaupt eine Herkunft eingetragen ist.
func (c *corsConfig) enabled() bool { return len(c.origins) > 0 }

// wrap ergänzt die CORS-Kopfzeilen und beantwortet Preflight-Anfragen.
func (c *corsConfig) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")

		if !c.allowed(origin) {
			// Kein Anti-CORS-Header, keine eigene Fehlermeldung: Ohne
			// Erlaubnis-Kopfzeile blockiert der Browser von sich aus. Eine
			// eigene Ablehnung würde nur verraten, dass es die Prüfung gibt.
			if r.Method == http.MethodOptions && origin != "" {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
			return
		}

		w.Header().Set("Access-Control-Allow-Origin", origin)
		// Sitzungs-Cookies müssen mitgehen, sonst nützt die Erlaubnis nichts.
		// Genau deshalb ist eine Platzhalter-Herkunft ausgeschlossen: Mit
		// Credentials verbietet der Standard "*" ohnehin.
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		// Antworten unterscheiden sich je Herkunft — ohne Vary liefert ein
		// Zwischenspeicher die Kopfzeile der falschen Herkunft aus.
		w.Header().Add("Vary", "Origin")

		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods",
				"GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers",
				"Content-Type, Authorization")
			w.Header().Set("Access-Control-Max-Age", "600")
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
