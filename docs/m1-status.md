# M1 — Status: abgeschlossen

**Ziel laut MVP-Plan:** „Ein Agent auf einem zweiten Rechner enrollt sich, erscheint in
der UI und übersteht einen Netzwerkausfall von 5 Minuten ohne manuellen Eingriff."

## Umgesetzt

| Bereich | Ergebnis |
|---------|----------|
| Protokoll v1 | 11 Nachrichtentypen, versioniert getrennt von der Produktversion (ADR-0016) |
| Transport | WebSocket, agent-initiiert, Heartbeat 20 s, Reconnect mit Backoff + Jitter |
| Liveness | Serverseitiges Aktivitätsfenster, tote Sitzungen werden abgeräumt (ADR-0019, neu) |
| Enrollment | Einmal-Token (15 min) → dauerhaftes Credential, Bestätigung in der UI nötig |
| Persistenz | Store-Schnittstelle + In-Memory-Implementierung (SQLite folgt in M2) |
| API | `/api/v1/hosts`, `/approve`, `/enroll-tokens`, `/events`, `/healthz` |
| Ereignisprotokoll | Jede Aktion mit Auslöser nachvollziehbar (ADR-0018) |

## Nachgewiesen durch Tests

- `TestEnrollmentFlow` — Token einlösen, Credential erhalten, unbestätigt keine Kommandos,
  Token wirkt nur einmal, nach Bestätigung Kommandos ausführbar
- `TestSameAgentSurvivesOutage` — dieselbe Agent-Instanz übersteht einen echten
  Verbindungsabbruch und ist danach ohne Zutun wieder handlungsfähig
- `TestReconnectAfterOutage` — tote Sitzungen werden serverseitig abgeräumt
- `TestProtocolMismatchIsFatal` — fatale Ablehnungen führen nicht in Endlosschleifen

End-to-End mit echten Prozessen durchlaufen: Token, Enrollment, Credential-Ablage mit
Rechten 600, Bestätigung, Neustart ohne Token, Ablehnung eines verbrauchten Tokens.

## Zwei Erkenntnisse aus dem Bau

1. **Das Liveness-Loch (ADR-0019).** Der Entwurf hatte nur clientseitige Heartbeats.
   Ein stumm verschwundener Agent wäre minutenlang als verbunden angezeigt worden.
2. **Die Test-Falle.** `httptest.Server.CloseClientConnections` lässt gehijackte
   WebSocket-Verbindungen unberührt — der erste Ausfalltest hat faktisch nichts geprüft.

## Bekannte Grenzen

- **In-Memory-Store:** Ein Neustart der Control Plane verliert alle Registrierungen.
  SQLite folgt in M2 (ADR-0005).
- **Kommandos werden quittiert, nicht ausgeführt** — der Docker-Provider ist M2.
- **Keine Nutzer-Authentifizierung:** Die API ist noch offen. Passwort-Login (Argon2id)
  kommt mit der UI in M2.
- **TLS optional:** läuft ohne Zertifikat im Klartext, mit Warnung. Für den Netzbetrieb
  Pflicht (ADR-0015).

## Nächster Schritt: M2

SQLite-Store, Docker-Provider (Container lesen, Lifecycle, Log-Streaming),
Report-Schleife, erste Web-Oberfläche. Ab M2 ist das Tool im eigenen Homelab nutzbar —
und ab da treibt der eigene Gebrauch das Produkt.
