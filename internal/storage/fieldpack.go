package storage

import (
	"bytes"
	"fmt"

	"github.com/klauspost/compress/zstd"
)

// Verschlüsselte Daten lassen sich nicht mehr komprimieren - sie sehen aus wie
// Zufall. Die großen Felder (Job- und SSH-Konsolen-Output) lagen deshalb
// größer in der Datenbank als ihr Klartext (Base64), und auch das Deflate im
// Backup-Archiv lief ins Leere. Komprimiert wird darum VOR dem Verschlüsseln.
//
// Ein eigenes Format-Kennzeichen braucht es nicht: Ein zstd-Frame beginnt mit
// der festen Magic 28 B5 2F FD. Als Text ist das kein gültiges UTF-8 (B5 ist
// ein Folgebyte ohne Startbyte) - ein gespeicherter Klartext kann so nie
// beginnen. Bestehende Werte bleiben damit ohne Umstellung lesbar.
//
// Längenhinweis: Wer Komprimieren und Verschlüsseln kombiniert, verrät über
// die Länge etwas über den Inhalt (CRIME/BREACH). Ausnutzbar ist das nur, wenn
// ein Angreifer eigenen Text neben ein Geheimnis in DASSELBE Feld bringt und
// die Länge wiederholt beobachten kann. Konsolen-Ausgaben sind bereits
// redigiert und liegen at rest - siehe docs/reference/security-model.

// packMinBytes ist die Größe, ab der sich Komprimieren lohnt. Kurze Felder
// (Namen, Hostnamen) würden durch den Frame-Kopf eher größer.
const packMinBytes = 512

// packMaxBytes begrenzt, wie groß ein entpackter Wert werden darf - Schutz
// vor einem manipulierten Wert, der beim Entpacken den Speicher füllt. Der
// größte reguläre Wert ist ein Agent-Output mit 4 MiB.
const packMaxBytes = 64 << 20

var zstdMagic = []byte{0x28, 0xb5, 0x2f, 0xfd}

var (
	zstdEncoder, _ = zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
	zstdDecoder, _ = zstd.NewReader(nil, zstd.WithDecoderConcurrency(0), zstd.WithDecoderMaxMemory(packMaxBytes))
)

// packField komprimiert einen Feldwert, wenn er groß genug ist und dadurch
// tatsächlich kleiner wird.
func packField(s string) string {
	if len(s) < packMinBytes {
		return s
	}
	packed := zstdEncoder.EncodeAll([]byte(s), nil)
	if len(packed) >= len(s) {
		return s
	}
	return string(packed)
}

// isPacked meldet, ob ein (entschlüsselter) Wert ein komprimierter ist.
func isPacked(s string) bool {
	return len(s) >= len(zstdMagic) && bytes.Equal([]byte(s[:len(zstdMagic)]), zstdMagic)
}

// unpackField kehrt packField um; unkomprimierte Werte kommen unverändert
// zurück.
func unpackField(s string) (string, error) {
	if !isPacked(s) {
		return s, nil
	}
	plain, err := zstdDecoder.DecodeAll([]byte(s), nil)
	if err != nil {
		return "", fmt.Errorf("feld entpacken: %w", err)
	}
	return string(plain), nil
}
