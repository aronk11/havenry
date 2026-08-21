# ADR-0029 — Teams über Rollen, nicht statt Rollen

**Status:** Akzeptiert · **Datum:** 2026-08-20

## Kontext
ADR-0022 gibt jedem Nutzer genau eine Rolle und eine Menge erlaubter Hosts.
Das trägt für ein bis fünf Personen. Sobald mehrere Leute dieselben Rechte
brauchen, wird es zur Fleißarbeit: Jeder neue Host muss bei jedem Nutzer
einzeln nachgetragen werden, und niemand kann beantworten, wer eigentlich
alles auf den Medienserver darf.

Zwei Wege:
1. Teams **ersetzen** die Direktzuweisung. Rechte gibt es nur noch über
   Mitgliedschaft.
2. Teams **ergänzen** sie. Die Direktzuweisung bleibt, Teams kommen dazu.

## Entscheidung
**Weg 2.** Ein Team bündelt Rolle und Host-Menge; Nutzer sind Mitglieder in
beliebig vielen Teams. Die Direktzuweisung am Nutzer bleibt bestehen.

**Die wirksamen Rechte sind die Vereinigung aus Direktzuweisung und allen
Teams.** Konkret:

- **Rolle:** die stärkste aller beteiligten Rollen (`admin` > `operator` > `viewer`).
- **Hosts:** die Vereinigung aller Host-Mengen. Eine leere Menge bedeutet
  weiterhin „alle Hosts" — steht sie irgendwo, gilt sie überall.

## Begründung gegen Weg 1
Teams als einzige Rechtequelle klingt sauberer, hat aber zwei harte Probleme:

- **Der erste Admin.** Beim ersten Start existiert niemand, der ein Team
  anlegen könnte. Ein automatisch erzeugtes Admin-Team wäre ein Sonderfall,
  der genau so aussieht wie die Direktzuweisung — nur mit mehr Umweg.
- **Der Einzelbetreiber.** Die überwiegende Mehrheit der Installationen hat
  einen Nutzer. Ihn zu zwingen, ein Team für sich selbst anzulegen, ist
  Zeremonie ohne Gegenwert.

## Bewusst additiv, nicht subtraktiv
Es gibt **keine verweigernden Regeln**. Ein Team kann Rechte nur hinzufügen,
nie entziehen. Verweigerungsregeln erzeugen Fragen, die niemand beantworten
kann („Warum darf ich das nicht, obwohl ich in Team X bin?") und deren Antwort
von der Auswertungsreihenfolge abhängt. Wer jemandem etwas wegnehmen will,
entfernt ihn aus dem Team oder senkt dessen Rolle.

## Konsequenzen
- Die Rechteprüfung braucht jetzt einen Auflösungsschritt: aus Nutzer plus
  Mitgliedschaften wird eine wirksame Identität. Das passiert an genau einer
  Stelle, nicht verstreut.
- **Änderungen an einem Team beenden die Sitzungen aller Mitglieder** — sonst
  trüge eine offene Sitzung alte Rechte weiter. Dasselbe Prinzip wie beim
  Rollenwechsel in ADR-0022.
- Das Ereignisprotokoll vermerkt bei einer Aktion künftig auch, über welchen
  Weg das Recht kam. Ohne das ist „wer durfte das und warum" nicht mehr
  beantwortbar, sobald Teams im Spiel sind.
- Der letzte Admin bleibt geschützt (ADR-0022). Die Prüfung muss jetzt auch
  Team-Admins mitzählen, sonst sperrt sich jemand aus, dessen Adminrecht
  ausschließlich aus einem Team stammt.
