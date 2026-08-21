# M2 — Status: abgeschlossen

**Ziel laut MVP-Plan:** „Kein SSH mehr nötig, um zu sehen, was auf allen Hosts läuft."

## Umgesetzt

| Bereich | Ergebnis |
|---------|----------|
| Persistenz | SQLite mit Migrationen, WAL, atomarem Token-Verbrauch (ADR-0005, ADR-0020) |
| Docker-Provider | Eigener Engine-API-Client über den Unix-Socket, ohne SDK |
| Zustandsmeldung | Agent meldet alle 15 s, zusätzlich sofort nach jeder Aktion |
| Kommandos | start/stop/restart, doppelt zugestellt unschädlich (ADR-0013) |
| Log-Streaming | Docker-Rahmenformat entpackt, per Server-Sent Events im Browser |
| Weboberfläche | Eingebettet ins Binary, Hosts, Stacks, Aktionen, Logs (ADR-0017) |
| API | `/stacks`, `/containers`, Aktionen, Log-Stream — versioniert unter `/api/v1` |

## Nachgewiesen

**Store-Konformitätstests** laufen gegen die Schnittstelle, nicht gegen eine
Implementierung — In-Memory und SQLite müssen dasselbe leisten. Darunter der
Wettlauf-Test: 16 gleichzeitige Einlösungen desselben Enrollment-Tokens, genau eine
darf gewinnen.

**Docker-Tests** laufen gegen einen selbstgebauten Daemon auf einem Unix-Socket.
Ohne den wären die Fehlerpfade — Container verschwindet, 304 Not Modified, kaputter
Log-Rahmen — faktisch ungetestet. Genau die treten im Homelab aber auf.

**End-to-End** mit echten Prozessen: Enrollment, Stack-Gruppierung nach
Compose-Labels, Ablehnung vor Bestätigung (HTTP 403), Bestätigung ohne Agent-Neustart,
Start eines gestoppten Containers, Wiederholung als No-Op, Log-Stream ohne
Rahmen-Steuerzeichen, Weboberfläche, und: **nach Neustart der Control Plane ist der
Host samt Bestätigung noch da** — das war in M1 noch nicht so.

## Erkenntnisse aus dem Bau

1. **Der Log-Rahmen.** Docker multiplext stdout und stderr ohne TTY in einen Strom
   mit 8-Byte-Kopf je Abschnitt. Wer ihn durchreicht, zeigt Steuerzeichen mitten im
   Text. Mit TTY entfällt der Kopf — beide Fälle brauchen Behandlung.
2. **Ein Kommentar war eine Lüge.** Der Code erzeugte CmdIDs mit Zeitstempel, der
   Kommentar behauptete das Gegenteil. Korrigiert: Der Zeitstempel ist richtig, denn
   zweimal Klicken heißt zwei Aktionen. Die Idempotenz gilt der Wiederholung derselben
   Nachricht, nicht dem zweiten Klick.
3. **CGO schlug bei der Auslieferung durch.** ADR-0020 hatte die Konsequenz benannt,
   das Makefile hatte sie nicht umgesetzt — die Control Plane startete nicht. Jetzt
   bauen Agent (CGO-frei) und Control Plane (vorerst mit CGO) getrennt.

## Bekannte Grenzen

- **Keine Nutzer-Authentifizierung.** Die API ist offen. Wer die Control Plane erreicht,
  kann Container steuern. Vor jedem Netzbetrieb zwingend nachzuholen.
- **CGO für die Control Plane**, bis der Treiberwechsel aus ADR-0020 erfolgt.
- **Metriken werden übertragen, aber nicht erhoben** — der Agent meldet noch keine
  CPU/RAM-Werte.
- **Kein Git, kein Drift** — das ist M3 und M4.
- **Ist-Zustand nur im Arbeitsspeicher:** Nach einem Neustart der Control Plane ist die
  Container-Sicht für wenige Sekunden leer, bis die Agenten neu melden. Bewusst so:
  veralteter Zustand wäre schlimmer als kurz keiner (ADR-0018).

## Nächster Schritt: M3

Git-Repo verbinden, klonen, pollen, Stacks nach Konvention erkennen (ADR-0014).
Danach M4 — der semantische Drift-Vergleich, das Herzstück des Produkts.
