# ADR-0019 — Serverseitiges Liveness-Fenster

**Status:** Akzeptiert · **Datum:** 2026-08-19

## Kontext
Beim Bau von M1 zeigte ein Test ein Loch im Entwurf: Ein Agent kann verschwinden,
ohne die Verbindung sauber zu schließen — Stromausfall, gekapptes WLAN, eingeschlafener
Host. TCP bemerkt das je nach Keepalive-Einstellung minutenlang nicht. Die Control Plane
hielt die Sitzung solange für lebendig und zeigte den Host als verbunden an.

Für ein Tool, dessen Kernversprechen Transparenz ist, ist das der schlimmstmögliche
Fehler: Es lügt über den Zustand, statt Unwissen einzugestehen.

## Entscheidung
Der Hub erzwingt ein Aktivitätsfenster von **drei Heartbeat-Perioden** (Default 60 s).
Bleibt jede Nachricht länger aus, gilt die Sitzung als tot, wird geschlossen und
abgeräumt. Der Agent bemerkt das seinerseits und verbindet sich neu (ADR-0013).

## Alternativen
- **TCP-Keepalive:** betriebssystemabhängig, in Containern schwer verlässlich einzustellen.
- **Nur clientseitiger Heartbeat:** erkennt nicht den Fall, dass der Client stumm weiterläuft.

## Konsequenzen
- Ein verbundener Host in der Oberfläche bedeutet: in der letzten Minute nachweislich am Leben.
- Sehr langsame Verbindungen brauchen ggf. angepasste Perioden (`SetPeriods`).
- **Lehre für Tests:** `httptest.Server.CloseClientConnections` fasst von WebSocket
  gehijackte Verbindungen nicht an. Ein Ausfalltest, der darauf baut, testet nichts.
  Die Testhilfe `flakyServer` kappt Verbindungen auf TCP-Ebene und deckt den Fall wirklich ab.
