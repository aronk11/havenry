# ADR-0018 — Observability der Plattform selbst

**Status:** Akzeptiert · **Datum:** 2026-08-19

## Kontext
Ein Monitoring-nahes Tool, das selbst intransparent ist, verliert sofort Vertrauen.
Gleichzeitig darf es keine Ressourcenfresser-Reputation bekommen — das ist in dieser
Community ein Ausschlusskriterium.

## Entscheidung
- **Metriken:** `/metrics` im Prometheus-Format (Agent-Verbindungen, Reconcile-Dauer,
  Drift-Zähler, Kommando-Fehlerrate, eigener Ressourcenverbrauch).
- **Ereignis-Log:** jede Aktion der Plattform (Reconcile, Update, Rollback, adopt/revert)
  wird als Event mit Auslöser gespeichert und ist in der UI einsehbar. Nutzer müssen
  nachvollziehen können, was die Plattform getan hat und warum.
- **Metrik-Aufbewahrung:** rohe Host-Metriken 24 h, danach aggregiert auf 5-Minuten-Werte
  für 30 Tage. Bewusst kurz — die Plattform ist kein Zeitreihen-Ersatz und verweist für
  echte Historie auf bestehende Lösungen.
- **Keine Telemetrie nach außen.** Kein Phone-Home, keine anonymen Nutzungsstatistiken,
  auch nicht opt-out. Nur opt-in Versionsprüfung, standardmäßig aus.

## Konsequenzen
- "Kein Phone-Home" gehört ins README — es ist in dieser Zielgruppe ein Kaufargument.
- Preis: keine Nutzungsdaten für Produktentscheidungen. Feedback muss aus der Community
  kommen, nicht aus Telemetrie.
