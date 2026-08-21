# ADR-0025 — Ratenlimit bei der Anmeldung

**Status:** Akzeptiert · **Datum:** 2026-08-19

## Kontext
Fehlversuche wurden protokolliert, aber nicht gebremst. Ein Protokoll hilft nur, wenn
jemand hineinsieht — im Homelab sieht typischerweise niemand hinein.

## Entscheidung
Fünf Fehlversuche innerhalb von 15 Minuten führen zu 15 Minuten Sperre. Gezählt wird
**getrennt nach Benutzername und nach Quelladresse**.

## Begründung der doppelten Zählung
- **Nur nach Adresse:** hilft nicht gegen Versuche aus vielen Quellen.
- **Nur nach Benutzername:** erlaubt es, ein fremdes Konto gezielt auszusperren, indem
  man absichtlich falsche Passwörter schickt.
- **Beides:** deckt die realistischen Fälle ab. Wird ein Konto durch fremde Versuche
  gesperrt, bleibt die eigene Adresse des legitimen Nutzers unbelastet — er kommt über
  einen anderen Benutzernamen oder nach Ablauf der Sperre wieder hinein.

Die Antwort ist `429` mit `Retry-After`, damit ein Client sinnvoll reagieren kann.

## Konsequenzen
- Die Zählung liegt im Arbeitsspeicher: Ein Neustart hebt Sperren auf. Für die
  Bedrohungslage ausreichend — ein Angreifer kann keinen Neustart auslösen.
- Ein Nutzer, der sein Passwort mehrfach falsch eingibt, muss warten. Das ist gewollt
  und wird in der Oberfläche mit der verbleibenden Zeit angezeigt.
- Die Tabelle wird ab 1000 Einträgen aufgeräumt, damit sie nicht als Speicherfalle
  dient.
