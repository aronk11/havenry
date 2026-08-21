package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Das Credential ist der einzige dauerhafte Zustand des Agenten.
//
// Wer die Datei hat, kann sich als dieser Host ausgeben — deshalb Rechte 600
// und ein atomarer Schreibvorgang: Ein halb geschriebenes Credential würde den
// Host dauerhaft aussperren, weil das Enrollment-Token bereits verbraucht ist
// (ADR-0015).

const credentialFile = "credential.json"

type credentialState struct {
	HostID     string `json:"host_id"`
	Credential string `json:"credential"`
}

func (a *Agent) credPath() string {
	return filepath.Join(a.cfg.StateDir, credentialFile)
}

func (a *Agent) loadCredential() (credentialState, error) {
	b, err := os.ReadFile(a.credPath())
	if errors.Is(err, os.ErrNotExist) {
		return credentialState{}, nil
	}
	if err != nil {
		return credentialState{}, fmt.Errorf("credential lesen: %w", err)
	}
	var st credentialState
	if err := json.Unmarshal(b, &st); err != nil {
		return credentialState{}, fmt.Errorf("credential dekodieren: %w", err)
	}
	return st, nil
}

// saveCredential legt das Credential ab. Datei nur für den Eigentümer lesbar —
// wer sie hat, kann sich als dieser Host ausgeben.
func (a *Agent) saveCredential(cred string) error {
	if err := os.MkdirAll(a.cfg.StateDir, 0o700); err != nil {
		return fmt.Errorf("state-dir anlegen: %w", err)
	}
	b, err := json.Marshal(credentialState{Credential: cred})
	if err != nil {
		return err
	}

	// Atomar schreiben: ein halb geschriebenes Credential würde den Host
	// dauerhaft aussperren, weil das Enrollment-Token schon verbraucht ist.
	tmp := a.credPath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return fmt.Errorf("credential schreiben: %w", err)
	}
	if err := os.Rename(tmp, a.credPath()); err != nil {
		return fmt.Errorf("credential ersetzen: %w", err)
	}
	a.cfg.Logger.Info("enrollment abgeschlossen, credential abgelegt", "pfad", a.credPath())
	return nil
}
