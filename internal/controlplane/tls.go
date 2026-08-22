package controlplane

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CertValidity ist die Laufzeit eines selbst erzeugten Zertifikats.
//
// Zehn Jahre statt der bei öffentlichen Zertifikaten üblichen 90 Tage: Ein
// selbstsigniertes Zertifikat wird ohnehin per Fingerprint vertraut (ADR-0015,
// TOFU). Ein Ablauf würde nur bedeuten, dass die Installation eines Tages
// ohne Zutun aufhört zu funktionieren — der schlechteste Fehlermodus für ein
// Werkzeug, das im Keller läuft und Monate nicht angefasst wird.
const CertValidity = 10 * 365 * 24 * time.Hour

// TLSPaths sind die Ablageorte im Datenverzeichnis.
type TLSPaths struct {
	Cert string
	Key  string
}

func tlsPaths(dataDir string) TLSPaths {
	return TLSPaths{
		Cert: filepath.Join(dataDir, "tls", "server.crt"),
		Key:  filepath.Join(dataDir, "tls", "server.key"),
	}
}

// EnsureTLS stellt sicher, dass ein Zertifikat vorliegt, und erzeugt bei
// Bedarf ein selbstsigniertes.
//
// Die Alternative — TLS optional lassen — hat sich als falsch erwiesen: Was
// optional ist, wird beim Einrichten übersprungen und dann jahrelang nicht
// nachgeholt. Bei einem Werkzeug, über das Passwörter und Kommandos an alle
// Hosts laufen, ist Klartext keine vertretbare Vorgabe.
//
// Rückgabe: Pfade und der SHA-256-Fingerprint zur Anzeige beim Start.
func EnsureTLS(dataDir string, hostnames []string) (TLSPaths, string, error) {
	p := tlsPaths(dataDir)

	if fileExists(p.Cert) && fileExists(p.Key) {
		fp, err := certFingerprint(p.Cert)
		return p, fp, err
	}

	if err := os.MkdirAll(filepath.Dir(p.Cert), 0o700); err != nil {
		return p, "", fmt.Errorf("tls-verzeichnis anlegen: %w", err)
	}

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return p, "", fmt.Errorf("schlüssel erzeugen: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return p, "", err
	}

	tmpl := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "havenry"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(CertValidity),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	// Alle plausiblen Namen und Adressen eintragen: Der Nutzer erreicht die
	// Control Plane mal über den Hostnamen, mal über die IP, mal über
	// localhost. Fehlt einer davon, meldet der Browser einen Zertifikatsfehler,
	// der wie ein Angriff aussieht, aber nur ein fehlender Eintrag ist.
	tmpl.DNSNames = append(tmpl.DNSNames, "localhost")
	tmpl.IPAddresses = append(tmpl.IPAddresses, net.ParseIP("127.0.0.1"), net.ParseIP("::1"))
	for _, h := range hostnames {
		if h == "" {
			continue
		}
		if ip := net.ParseIP(h); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		} else {
			tmpl.DNSNames = append(tmpl.DNSNames, h)
		}
	}
	tmpl.IPAddresses = append(tmpl.IPAddresses, localAddresses()...)

	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		return p, "", fmt.Errorf("zertifikat erzeugen: %w", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := os.WriteFile(p.Cert, certPEM, 0o644); err != nil { //nolint:gosec // G306: Zertifikat ist öffentlich, anders als der Schlüssel unten
		return p, "", fmt.Errorf("zertifikat schreiben: %w", err)
	}

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return p, "", err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	// Nur für den Eigentümer lesbar — der private Schlüssel ist das Geheimnis,
	// auf dem die gesamte Transportsicherheit beruht.
	if err := os.WriteFile(p.Key, keyPEM, 0o600); err != nil {
		return p, "", fmt.Errorf("schlüssel schreiben: %w", err)
	}

	sum := sha256.Sum256(der)
	return p, formatFingerprint(sum[:]), nil
}

// localAddresses sammelt die IPv4-Adressen der Maschine.
func localAddresses() []net.IP {
	var out []net.IP
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return out
	}
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok || ipnet.IP.IsLoopback() {
			continue
		}
		if v4 := ipnet.IP.To4(); v4 != nil {
			out = append(out, v4)
		}
	}
	return out
}

func certFingerprint(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	block, _ := pem.Decode(b)
	if block == nil {
		return "", fmt.Errorf("zertifikat %q ist kein gültiges PEM", path)
	}
	sum := sha256.Sum256(block.Bytes)
	return formatFingerprint(sum[:]), nil
}

// formatFingerprint gibt den Fingerprint in der üblichen Doppelpunkt-Schreibweise
// aus, damit er sich mit der Anzeige im Browser vergleichen lässt.
func formatFingerprint(sum []byte) string {
	h := hex.EncodeToString(sum)
	parts := make([]string, 0, len(h)/2)
	for i := 0; i < len(h); i += 2 {
		parts = append(parts, strings.ToUpper(h[i:i+2]))
	}
	return strings.Join(parts, ":")
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}
