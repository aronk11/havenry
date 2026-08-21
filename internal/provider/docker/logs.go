package docker

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// LogStream liefert Log-Zeilen eines Containers.
type LogStream interface {
	// Next liefert den nächsten Abschnitt. io.EOF beendet den Stream.
	Next() (LogEntry, error)
	Close() error
}

// LogEntry ist ein Abschnitt Log-Ausgabe.
type LogEntry struct {
	// Stderr unterscheidet die Quelle. Bei TTY-Containern immer false,
	// weil Docker dort beide Ströme zusammenlegt.
	Stderr bool
	Data   []byte
}

// Stream-Kennungen im Rahmenkopf.
const (
	streamStdin  = 0
	streamStdout = 1
	streamStderr = 2
)

const frameHeaderSize = 8

// demuxReader entpackt Dockers Log-Format.
//
// Ohne TTY multiplext Docker stdout und stderr in einen Strom und stellt jedem
// Abschnitt einen 8-Byte-Kopf voran: [Stream, 0, 0, 0, Länge (4 Byte, big endian)].
// Mit TTY entfällt der Kopf und die Daten kommen roh.
//
// Diese Unterscheidung ist die klassische Stolperstelle: Wer den Strom einfach
// durchreicht, bekommt bei Nicht-TTY-Containern alle acht Bytes Kopfdaten als
// Steuerzeichen mitten im Text angezeigt.
type demuxReader struct {
	rc  io.ReadCloser
	br  *bufio.Reader
	tty bool
	// probed merkt sich, ob das Format schon bestimmt wurde.
	probed bool
}

func newDemuxReader(rc io.ReadCloser) *demuxReader {
	return &demuxReader{rc: rc, br: bufio.NewReaderSize(rc, 32*1024)}
}

// probe bestimmt anhand der ersten Bytes, ob der Strom gerahmt ist.
//
// Ein gültiger Rahmenkopf beginnt mit einer bekannten Stream-Kennung, gefolgt
// von drei Nullbytes. Diese Kombination am Anfang echter Log-Ausgabe ist
// praktisch ausgeschlossen — Logtext beginnt nicht mit Nullbytes.
func (d *demuxReader) probe() error {
	head, err := d.br.Peek(frameHeaderSize)
	if err != nil {
		if errors.Is(err, io.EOF) {
			// Zu wenig Daten für einen Kopf: als roh behandeln.
			d.tty, d.probed = true, true
			return nil
		}
		return err
	}

	framed := (head[0] == streamStdin || head[0] == streamStdout || head[0] == streamStderr) &&
		head[1] == 0 && head[2] == 0 && head[3] == 0
	d.tty = !framed
	d.probed = true
	return nil
}

func (d *demuxReader) Next() (LogEntry, error) {
	if !d.probed {
		if err := d.probe(); err != nil {
			return LogEntry{}, err
		}
	}

	if d.tty {
		buf := make([]byte, 8192)
		n, err := d.br.Read(buf)
		if n > 0 {
			return LogEntry{Data: buf[:n]}, nil
		}
		return LogEntry{}, err
	}

	var header [frameHeaderSize]byte
	if _, err := io.ReadFull(d.br, header[:]); err != nil {
		return LogEntry{}, err
	}

	size := binary.BigEndian.Uint32(header[4:])
	if size == 0 {
		return LogEntry{Stderr: header[0] == streamStderr}, nil
	}
	// Schutz gegen eine absurde Längenangabe: ein einzelner Abschnitt über
	// 16 MB deutet auf einen kaputten Strom hin, nicht auf echte Logs.
	if size > 16<<20 {
		return LogEntry{}, fmt.Errorf("log-abschnitt mit unplausibler länge %d", size)
	}

	data := make([]byte, size)
	if _, err := io.ReadFull(d.br, data); err != nil {
		return LogEntry{}, err
	}
	return LogEntry{Stderr: header[0] == streamStderr, Data: data}, nil
}

func (d *demuxReader) Close() error { return d.rc.Close() }

var _ LogStream = (*demuxReader)(nil)
