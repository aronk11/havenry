package auth

import "sort"

// Grant ist eine einzelne Rechtequelle — die Direktzuweisung am Nutzer oder
// die Mitgliedschaft in einem Team (ADR-0029).
type Grant struct {
	// Source benennt die Herkunft für das Ereignisprotokoll: "direct" oder
	// der Teamname. Ohne diese Angabe ist "wer durfte das und warum" nicht
	// mehr beantwortbar, sobald Teams im Spiel sind.
	Source string
	Role   Role
	// HostIDs ist die erlaubte Host-Menge. Leer bedeutet: alle Hosts.
	HostIDs []string
}

// roleRank ordnet die Rollen. Nur hier steht, welche stärker ist — eine
// Verzweigung an anderer Stelle wäre eine zweite Wahrheit.
func roleRank(r Role) int {
	switch r {
	case RoleAdmin:
		return 3
	case RoleOperator:
		return 2
	case RoleViewer:
		return 1
	default:
		return 0
	}
}

// Resolve verschmilzt alle Rechtequellen zu einer wirksamen Identität.
//
// Vereinigung, niemals Schnittmenge: Teams können Rechte nur hinzufügen, nie
// entziehen (ADR-0029). Verweigerungsregeln würden Fragen erzeugen, deren
// Antwort von der Auswertungsreihenfolge abhängt.
//
//   - Rolle: die stärkste aller Quellen.
//   - Hosts: die Vereinigung. Ist irgendwo die Menge leer ("alle Hosts"),
//     gilt das für die gesamte Identität — sonst würde eine Quelle, die alles
//     erlaubt, durch eine engere eingeschränkt, und das wäre Entzug.
func Resolve(userID, username string, grants []Grant) Identity {
	id := Identity{UserID: userID, Username: username, Role: ""}

	seen := map[string]bool{}
	unrestricted := false
	var hosts []string

	for _, g := range grants {
		if !g.Role.Valid() {
			// Eine unbekannte Rolle trägt nichts bei, statt alles zu erlauben.
			continue
		}
		if roleRank(g.Role) > roleRank(id.Role) {
			id.Role = g.Role
		}
		id.Sources = append(id.Sources, g.Source)

		if len(g.HostIDs) == 0 {
			unrestricted = true
			continue
		}
		for _, h := range g.HostIDs {
			if !seen[h] {
				seen[h] = true
				hosts = append(hosts, h)
			}
		}
	}

	if !unrestricted {
		sort.Strings(hosts)
		id.HostIDs = hosts
	}
	// Bei unrestricted bleibt HostIDs leer — das ist die Kennzeichnung für
	// "alle Hosts" (siehe CanAccessHost).

	sort.Strings(id.Sources)
	return id
}
