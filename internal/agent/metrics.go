package agent

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/aronk11/havenry/internal/transport"
)

// metricsReader liest Host-Metriken.
//
// Bewusst direkt aus /proc statt über eine Bibliothek: Es sind vier Dateien,
// das Format ist seit Jahrzehnten stabil, und der Agent bleibt ohne zusätzliche
// Abhängigkeit klein (ADR-0005). Auf Nicht-Linux-Systemen liefert er einen
// Fehler statt geratener Werte.
//
// Der Zustand für die CPU-Differenzbildung gehört bewusst hierher und nicht in
// eine Paketvariable: Globaler veränderlicher Zustand wäre ohne Sperre
// datenrennenanfällig und ließe sich nicht testen.
type metricsReader struct {
	mu       sync.Mutex
	lastIdle uint64
	lastAll  uint64
}

// Read erfasst CPU-, Speicher- und Plattenauslastung.
func (mr *metricsReader) Read(diskPath string) (transport.MetricsReport, error) {
	m := transport.MetricsReport{ObservedAt: time.Now().UTC()}

	cpu, err := mr.cpuPercent()
	if err != nil {
		return m, err
	}
	m.CPUPercent = cpu

	if used, total, err := memInfo(); err == nil {
		m.MemUsed, m.MemTotal = used, total
	}
	if used, total, err := diskUsage(diskPath); err == nil {
		m.DiskUsed, m.DiskTotal = used, total
	}
	if up, err := uptimeSeconds(); err == nil {
		m.UptimeSecs = up
	}
	m.LoadAverage = loadAverage()
	return m, nil
}

// cpuPercent bestimmt die Auslastung als Differenz zweier Messungen.
//
// /proc/stat liefert Summen seit dem Systemstart. Der erste Aufruf kann daher
// keinen sinnvollen Wert liefern und meldet 0.
func (mr *metricsReader) cpuPercent() (float64, error) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return 0, fmt.Errorf("cpu-werte nicht lesbar (kein linux?): %w", err)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	if !sc.Scan() {
		return 0, fmt.Errorf("/proc/stat ist leer")
	}
	fields := strings.Fields(sc.Text())
	if len(fields) < 5 || fields[0] != "cpu" {
		return 0, fmt.Errorf("/proc/stat unerwartet aufgebaut")
	}

	var total, idle uint64
	for i, f := range fields[1:] {
		v, err := strconv.ParseUint(f, 10, 64)
		if err != nil {
			continue
		}
		total += v
		// Feld 4 ist idle, Feld 5 iowait — beides zählt als untätig.
		if i == 3 || i == 4 {
			idle += v
		}
	}

	mr.mu.Lock()
	prevIdle, prevTotal := mr.lastIdle, mr.lastAll
	mr.lastIdle, mr.lastAll = idle, total
	mr.mu.Unlock()

	if prevTotal == 0 || total <= prevTotal {
		return 0, nil
	}
	dTotal := float64(total - prevTotal)
	dIdle := float64(idle - prevIdle)
	return (1 - dIdle/dTotal) * 100, nil
}

func memInfo() (used, total uint64, err error) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()

	vals := map[string]uint64{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		parts := strings.Fields(sc.Text())
		if len(parts) < 2 {
			continue
		}
		key := strings.TrimSuffix(parts[0], ":")
		v, err := strconv.ParseUint(parts[1], 10, 64)
		if err != nil {
			continue
		}
		vals[key] = v * 1024 // /proc/meminfo rechnet in kB
	}

	total = vals["MemTotal"]
	// MemAvailable ist der richtige Wert: MemFree ignoriert Cache, der bei
	// Bedarf freigegeben wird, und ließe jeden Linux-Host als voll erscheinen.
	avail, ok := vals["MemAvailable"]
	if !ok {
		avail = vals["MemFree"] + vals["Cached"] + vals["Buffers"]
	}
	if total == 0 {
		return 0, 0, fmt.Errorf("MemTotal fehlt")
	}
	if avail > total {
		avail = total
	}
	return total - avail, total, nil
}

func diskUsage(path string) (used, total uint64, err error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, err
	}
	bs := uint64(st.Bsize)
	total = st.Blocks * bs
	// Bavail statt Bfree: Ein Teil ist für root reserviert und für den
	// Nutzer nicht verfügbar.
	free := st.Bavail * bs
	if free > total {
		free = total
	}
	return total - free, total, nil
}

func uptimeSeconds() (uint64, error) {
	b, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(string(b))
	if len(fields) == 0 {
		return 0, fmt.Errorf("/proc/uptime leer")
	}
	f, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, err
	}
	return uint64(f), nil
}

func loadAverage() []float64 {
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return nil
	}
	fields := strings.Fields(string(b))
	if len(fields) < 3 {
		return nil
	}
	out := make([]float64, 0, 3)
	for _, f := range fields[:3] {
		v, err := strconv.ParseFloat(f, 64)
		if err != nil {
			return nil
		}
		out = append(out, v)
	}
	return out
}
