package gitsync

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/goccy/go-yaml"
)

// StacksDir ist das Wurzelverzeichnis der Konvention aus ADR-0014.
const StacksDir = "stacks"

// composeNames sind die von Docker Compose akzeptierten Dateinamen,
// in der Reihenfolge, in der Compose selbst sucht.
var composeNames = []string{
	"compose.yaml", "compose.yml",
	"docker-compose.yaml", "docker-compose.yml",
}

// Mode bestimmt, wie mit Abweichungen umgegangen wird (ADR-0004).
type Mode string

const (
	// ModeObserve ist der Vorgabewert: Abweichungen werden angezeigt,
	// niemals automatisch angeglichen.
	ModeObserve Mode = "observe"
	// ModeApply gleicht kontinuierlich an Git an. Ausdrücklich einzuschalten.
	ModeApply Mode = "apply"
)

func (m Mode) Valid() bool { return m == ModeObserve || m == ModeApply }

// UpdatePolicy steuert den Umgang mit verfügbaren Image-Aktualisierungen.
type UpdatePolicy string

const (
	UpdateNotify UpdatePolicy = "notify"
	UpdateAuto   UpdatePolicy = "auto"
	UpdateOff    UpdatePolicy = "off"
)

// StackFile ist der Inhalt der optionalen stack.yaml (ADR-0014).
//
// Rein additiv: Ohne sie funktioniert alles nach Konvention. Sie ist der
// einzige plattformspezifische Artefakttyp und bleibt bewusst winzig.
type StackFile struct {
	Hosts        []string     `yaml:"hosts"`
	Mode         Mode         `yaml:"mode"`
	Updates      UpdatePolicy `yaml:"updates"`
	HealthWindow string       `yaml:"health_window"`
}

// Stack ist ein im Repo gefundener Stack.
type Stack struct {
	// Name ist der Verzeichnisname unterhalb des Hostverzeichnisses.
	Name string
	// Hosts sind die Zielhosts. Aus der Konvention der Verzeichnisstruktur,
	// gegebenenfalls überschrieben durch stack.yaml.
	Hosts []string
	// ComposePath ist der Pfad relativ zur Repo-Wurzel.
	ComposePath  string
	Mode         Mode
	Updates      UpdatePolicy
	HealthWindow time.Duration
	// EnvExample listet Variablen aus .env.example — daraus kann die
	// Oberfläche anzeigen, was ein Stack erwartet, ohne je einen Wert zu kennen
	// (ADR-0006).
	EnvExample []string
}

// Problem beschreibt etwas, das im Repo nicht stimmt.
//
// Bewusst gesammelt statt geworfen: Ein einzelnes kaputtes Verzeichnis darf
// nicht dazu führen, dass gar keine Stacks erkannt werden. Der Nutzer soll
// sehen, was funktioniert *und* was hakt.
type Problem struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

// Discovery ist das Ergebnis eines Durchlaufs.
type Discovery struct {
	Stacks   []Stack
	Problems []Problem
}

// Discover durchsucht die Arbeitskopie nach Stacks.
//
// Erwartete Struktur (ADR-0014):
//
//	<basePath>/stacks/<hostname>/<stackname>/compose.yaml
//
// Alles darüber hinaus wird über die optionale stack.yaml ausgedrückt.
func Discover(root, basePath string) (Discovery, error) {
	var d Discovery

	stacksRoot := filepath.Join(root, basePath, StacksDir)
	info, err := os.Stat(stacksRoot)
	if err != nil || !info.IsDir() {
		return d, fmt.Errorf("verzeichnis %q nicht gefunden — erwartet wird %s/<host>/<stack>/compose.yaml",
			filepath.Join(basePath, StacksDir), StacksDir)
	}

	hostDirs, err := os.ReadDir(stacksRoot)
	if err != nil {
		return d, err
	}

	for _, hostDir := range hostDirs {
		if !hostDir.IsDir() || strings.HasPrefix(hostDir.Name(), ".") {
			continue
		}
		hostName := hostDir.Name()

		stackDirs, err := os.ReadDir(filepath.Join(stacksRoot, hostName))
		if err != nil {
			d.Problems = append(d.Problems, Problem{Path: hostName, Message: err.Error()})
			continue
		}

		for _, stackDir := range stackDirs {
			if !stackDir.IsDir() || strings.HasPrefix(stackDir.Name(), ".") {
				continue
			}
			rel := filepath.Join(StacksDir, hostName, stackDir.Name())
			abs := filepath.Join(stacksRoot, hostName, stackDir.Name())

			stack, prob := readStack(abs, rel, hostName, stackDir.Name())
			if prob != nil {
				d.Problems = append(d.Problems, *prob)
				continue
			}
			d.Stacks = append(d.Stacks, *stack)
		}
	}

	// Stabile Reihenfolge — sonst springen Einträge in der Oberfläche.
	sort.Slice(d.Stacks, func(i, j int) bool {
		if d.Stacks[i].Hosts[0] != d.Stacks[j].Hosts[0] {
			return d.Stacks[i].Hosts[0] < d.Stacks[j].Hosts[0]
		}
		return d.Stacks[i].Name < d.Stacks[j].Name
	})
	return d, nil
}

func readStack(abs, rel, hostName, stackName string) (*Stack, *Problem) {
	composeFile := ""
	for _, name := range composeNames {
		if _, err := os.Stat(filepath.Join(abs, name)); err == nil {
			composeFile = name
			break
		}
	}
	if composeFile == "" {
		return nil, &Problem{
			Path:    rel,
			Message: "keine compose-datei gefunden (erwartet: " + strings.Join(composeNames, ", ") + ")",
		}
	}

	stack := &Stack{
		Name:        stackName,
		Hosts:       []string{hostName},
		ComposePath: filepath.ToSlash(filepath.Join(rel, composeFile)),
		Mode:        ModeObserve, // Vorgabe nach ADR-0004
		Updates:     UpdateNotify,
	}

	if b, err := os.ReadFile(filepath.Join(abs, "stack.yaml")); err == nil {
		var sf StackFile
		if err := yaml.Unmarshal(b, &sf); err != nil {
			return nil, &Problem{Path: rel + "/stack.yaml", Message: "unlesbar: " + err.Error()}
		}
		if len(sf.Hosts) > 0 {
			stack.Hosts = sf.Hosts
		}
		if sf.Mode != "" {
			if !sf.Mode.Valid() {
				return nil, &Problem{
					Path:    rel + "/stack.yaml",
					Message: fmt.Sprintf("mode %q unbekannt (erlaubt: observe, apply)", sf.Mode),
				}
			}
			stack.Mode = sf.Mode
		}
		if sf.Updates != "" {
			switch sf.Updates {
			case UpdateNotify, UpdateAuto, UpdateOff:
				stack.Updates = sf.Updates
			default:
				return nil, &Problem{
					Path:    rel + "/stack.yaml",
					Message: fmt.Sprintf("updates %q unbekannt (erlaubt: notify, auto, off)", sf.Updates),
				}
			}
		}
		if sf.HealthWindow != "" {
			dur, err := time.ParseDuration(sf.HealthWindow)
			if err != nil {
				return nil, &Problem{
					Path:    rel + "/stack.yaml",
					Message: "health_window unlesbar: " + err.Error(),
				}
			}
			stack.HealthWindow = dur
		}
	}

	stack.EnvExample = readEnvKeys(filepath.Join(abs, ".env.example"))
	return stack, nil
}

// readEnvKeys liest nur die Namen der Variablen, nie deren Werte (ADR-0006).
func readEnvKeys(path string) []string {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var keys []string
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if i := strings.Index(line, "="); i > 0 {
			keys = append(keys, strings.TrimSpace(line[:i]))
		}
	}
	return keys
}

// ReadCompose liest den Inhalt einer Compose-Datei aus der Arbeitskopie.
//
// Der Pfad wird gegen die Wurzel geprüft: Ein Pfad aus dem Repo darf niemals
// aus der Arbeitskopie herausführen.
func ReadCompose(root, relPath string) ([]byte, error) {
	clean := filepath.Clean(filepath.Join(root, relPath))
	rootClean := filepath.Clean(root)
	if !strings.HasPrefix(clean, rootClean+string(os.PathSeparator)) {
		return nil, fmt.Errorf("pfad %q liegt außerhalb der arbeitskopie", relPath)
	}
	return os.ReadFile(clean)
}
