package services

import (
	"archive/zip"
	"bufio"
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"filippo.io/age"
	"golang.org/x/crypto/scrypt"
)

// Backup-Archivformat: eine verschlüsselte Datei, die intern ein ZIP mehrerer
// Dateien (DB, Master-Key, Config, TLS) trägt. Zwei Verschlüsselungen:
//
//   - Empfänger-Schlüssel (age, X25519): das ZIP wird an einen oder mehrere
//     öffentliche Schlüssel verschlüsselt; der private Schlüssel liegt nie auf
//     dem Server (writeAgeArchive). Erkennbar an der ersten Zeile
//     "age-encryption.org/v1".
//   - Passphrase (LCMBAK): scrypt-Ableitung + AES-256-GCM, siehe unten.
//
// Aktuelles Format (LCMBAK3) - blockweise verschlüsselt, damit weder beim
// Schreiben noch beim Lesen das ganze Archiv im Speicher liegt:
//
//	magic(8) || salt(16) || nonceBase(8) || [ len(4) || AES-256-GCM(block) ]...
//
// Der Nonce eines Blocks ist nonceBase(8) || Blocknummer(4, big endian); der
// letzte Block trägt ein anderes AAD-Byte als alle vorherigen. Damit fällt ein
// abgeschnittenes Archiv beim Entschlüsseln auf und wird nicht als vollständig
// durchgewunken.
//
// Ältere Formate bleiben lesbar, damit bestehende Backups restaurierbar
// bleiben. Sie tragen das gesamte Archiv in EINEM GCM-Siegel und brauchen
// deshalb weiterhin Speicher in Archivgröße:
//
//	LCMBAK1: scrypt N=2^15, LCMBAK2: scrypt N=2^17 - je magic(8) || salt(16) || nonce(12) || GCM(zip)
//
// Der AES-Schlüssel wird per scrypt aus der Passphrase + zufälligem Salt
// abgeleitet - die Passphrase selbst wird nirgends gespeichert. Ohne die
// korrekte Passphrase schlägt das GCM-Auth-Tag fehl (kein Entschlüsseln).
var (
	backupMagicV1 = []byte("LCMBAK1\n") // scrypt N=2^15, ein Siegel - nur noch lesen
	backupMagicV2 = []byte("LCMBAK2\n") // scrypt N=2^17, ein Siegel - nur noch lesen
	backupMagicV3 = []byte("LCMBAK3\n") // scrypt N=2^16, blockweise - aktuelles Schreibformat
	backupMagic   = backupMagicV3       // Schreibformat
)

const (
	backupMagicLen = 8
	backupSaltLen  = 16
	// nonceBaseLen + 4 Byte Blocknummer ergeben den 12-Byte-GCM-Nonce.
	nonceBaseLen  = 8
	scryptNLegacy = 1 << 15 // 32768  - LCMBAK1
	scryptNSealed = 1 << 17 // 131072 - LCMBAK2
	// scryptNStream (2^16 = 64 MiB) ist der Kostenparameter des aktuellen
	// Formats. Bewusst niedriger als LCMBAK2: scrypt belegt N*r*128 Byte am
	// Stück, und diese Spitze fällt auf DEM Rechner an, der sichert oder
	// zurückspielt - oft eine kleine VM. 128 MiB allein für die
	// Schlüsselableitung waren dort zu viel; 64 MiB bleiben deutlich über
	// dem üblichen interaktiven Richtwert (16 MiB).
	scryptNStream = 1 << 16
	scryptR       = 8
	scryptP       = 1
	scryptKeyLen  = 32
	// archiveChunkSize ist die Klartextgröße eines Blocks. Sie bestimmt den
	// Speicherbedarf beim Ver-/Entschlüsseln - unabhängig von der Archivgröße.
	archiveChunkSize = 1 << 20 // 1 MiB
	// maxChunkCipherLen begrenzt die aus dem Archiv gelesene Blocklänge, damit
	// eine manipulierte Längenangabe keine riesige Zuteilung auslöst.
	maxChunkCipherLen = archiveChunkSize + 4096
	maxArchiveSize    = 512 << 20 // 512 MiB Sicherheitslimit für die Altformate
)

// AAD-Marken: unterscheiden einen Zwischenblock vom letzten Block.
var (
	aadChunk = []byte{0}
	aadFinal = []byte{1}
)

// scryptNForMagic liefert den scrypt-Kostenparameter zum Archiv-Magic, oder 0
// wenn das Magic nicht erkannt wird.
func scryptNForMagic(magic []byte) int {
	switch {
	case bytes.Equal(magic, backupMagicV3):
		return scryptNStream
	case bytes.Equal(magic, backupMagicV2):
		return scryptNSealed
	case bytes.Equal(magic, backupMagicV1):
		return scryptNLegacy
	default:
		return 0
	}
}

var (
	// ErrBackupPassphrase signalisiert eine falsche Passphrase oder ein
	// beschädigtes/fremdes Archiv (nicht unterscheidbar - bewusst generisch).
	ErrBackupPassphrase = errors.New("falsche passphrase oder beschädigtes backup")
	ErrBackupFormat     = errors.New("kein gültiges LCM-backup-archiv")
)

// archiveFile ist eine einzelne Datei im Backup-Archiv (Inhalt im Speicher).
type archiveFile struct {
	Name string // Name im Archiv (z.B. "app.db", "lcm.key")
	Data []byte
}

// archiveSource beschreibt eine zu sichernde Datei. Path wird streamend
// gelesen - für die Datenbank der entscheidende Unterschied, sie ist mit
// Abstand die größte Datei im Archiv. Data ist die Variante für kleine
// Inhalte, die ohnehin schon im Speicher liegen.
type archiveSource struct {
	Name string
	Path string
	Data []byte
}

// writeEncryptedArchive schreibt die Quellen als verschlüsseltes Archiv nach
// dst. Weder das ZIP noch das Siegel liegen dabei vollständig im Speicher:
// Der Bedarf ist der Blockpuffer plus die Fenster von deflate - unabhängig
// davon, ob die Datenbank 10 MB oder 10 GB groß ist.
func writeEncryptedArchive(dst io.Writer, sources []archiveSource, passphrase string) error {
	if passphrase == "" {
		return errors.New("passphrase erforderlich")
	}
	salt := make([]byte, backupSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return err
	}
	nonceBase := make([]byte, nonceBaseLen)
	if _, err := rand.Read(nonceBase); err != nil {
		return err
	}
	aead, err := deriveAEAD(passphrase, salt, scryptNStream)
	if err != nil {
		return err
	}
	if aead.NonceSize() != nonceBaseLen+4 {
		return fmt.Errorf("unerwartete nonce-größe %d", aead.NonceSize())
	}
	for _, part := range [][]byte{backupMagicV3, salt, nonceBase} {
		if _, err := dst.Write(part); err != nil {
			return err
		}
	}

	cw := newChunkWriter(dst, aead, nonceBase)
	if err := packSources(cw, sources); err != nil {
		return err
	}
	return cw.Close()
}

// packSources schreibt die Quellen als ZIP in w - der gemeinsame Kern beider
// Archivformate. Dateien werden streamend gelesen, nie am Stück geladen.
func packSources(w io.Writer, sources []archiveSource) error {
	zw := zip.NewWriter(w)
	for _, src := range sources {
		zf, err := zw.Create(src.Name)
		if err != nil {
			return err
		}
		if src.Path != "" {
			f, err := os.Open(src.Path)
			if err != nil {
				return fmt.Errorf("%s lesen: %w", src.Name, err)
			}
			_, err = io.Copy(zf, f)
			f.Close()
			if err != nil {
				return fmt.Errorf("%s packen: %w", src.Name, err)
			}
			continue
		}
		if _, err := zf.Write(src.Data); err != nil {
			return err
		}
	}
	return zw.Close()
}

// ageMagic ist die erste Zeile jedes age-Archivs (Empfänger-Schlüssel).
const ageMagic = "age-encryption.org/v1\n"

// writeAgeArchive verschlüsselt das ZIP an die Empfänger - X25519 nach dem
// age-Format. age verschlüsselt in 64-KiB-Blöcken mit ChaCha20-Poly1305 und
// bindet das Ende des Stroms ein: Ein abgeschnittenes Archiv fällt beim
// Entschlüsseln auf. Es gibt keine Passphrase; wer das Archiv öffnen will,
// braucht den privaten Schlüssel eines Empfängers.
func writeAgeArchive(dst io.Writer, sources []archiveSource, recipients []age.Recipient) error {
	if len(recipients) == 0 {
		return errors.New("keine empfänger")
	}
	w, err := age.Encrypt(dst, recipients...)
	if err != nil {
		return err
	}
	if err := packSources(w, sources); err != nil {
		return err
	}
	return w.Close()
}

// archiveKey ist, womit ein Archiv geöffnet wird: die Passphrase (LCMBAK-
// Format) oder ein privater age-Schlüssel (Empfänger-Format).
type archiveKey struct {
	passphrase string
	identities []age.Identity
}

// keyFromSecret nimmt das, was ein Mensch in das Feld „Passphrase oder
// privater Schlüssel" tippt, und erkennt selbst, was es ist: Ein privater
// age-Schlüssel beginnt mit AGE-SECRET-KEY-1.
func keyFromSecret(secret string) archiveKey {
	secret = strings.TrimSpace(secret)
	if strings.HasPrefix(strings.ToUpper(secret), "AGE-SECRET-KEY-1") {
		// Bech32 kennt nur ganz groß oder ganz klein; age will groß.
		ids, err := age.ParseIdentities(strings.NewReader(strings.ToUpper(secret)))
		if err == nil && len(ids) > 0 {
			return archiveKey{identities: ids}
		}
	}
	return archiveKey{passphrase: secret}
}

func (k archiveKey) empty() bool { return k.passphrase == "" && len(k.identities) == 0 }

// ErrBackupNoIdentity: Das Archiv ist an Empfänger-Schlüssel verschlüsselt,
// mitgegeben wurde aber nur eine Passphrase (oder gar nichts).
var ErrBackupNoIdentity = errors.New("das archiv ist an empfänger-schlüssel verschlüsselt - der private schlüssel eines empfängers ist erforderlich")

// chunkWriter verschlüsselt den Datenstrom blockweise. Jeder volle Block wird
// sofort geschrieben; Close() versiegelt den Rest als Abschlussblock.
type chunkWriter struct {
	dst    io.Writer
	aead   cipher.AEAD
	base   []byte
	buf    []byte
	out    []byte // wiederverwendeter Puffer für den Chiffretext
	blocks uint32
	closed bool
}

func newChunkWriter(dst io.Writer, aead cipher.AEAD, base []byte) *chunkWriter {
	return &chunkWriter{
		dst:  dst,
		aead: aead,
		base: base,
		buf:  make([]byte, 0, archiveChunkSize),
		out:  make([]byte, 0, archiveChunkSize+aead.Overhead()+4),
	}
}

func (w *chunkWriter) Write(p []byte) (int, error) {
	total := len(p)
	for len(p) > 0 {
		free := archiveChunkSize - len(w.buf)
		n := min(free, len(p))
		w.buf = append(w.buf, p[:n]...)
		p = p[n:]
		if len(w.buf) == archiveChunkSize {
			if err := w.flush(aadChunk); err != nil {
				return total - len(p), err
			}
		}
	}
	return total, nil
}

// Close schreibt den Abschlussblock. Er trägt ein anderes AAD und markiert so
// das Ende - ein abgeschnittenes Archiv fällt beim Lesen auf.
func (w *chunkWriter) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	return w.flush(aadFinal)
}

func (w *chunkWriter) flush(aad []byte) error {
	nonce := make([]byte, nonceBaseLen+4)
	copy(nonce, w.base)
	binary.BigEndian.PutUint32(nonce[nonceBaseLen:], w.blocks)
	w.blocks++

	sealed := w.aead.Seal(w.out[:0], nonce, w.buf, aad)
	var header [4]byte
	binary.BigEndian.PutUint32(header[:], uint32(len(sealed)))
	if _, err := w.dst.Write(header[:]); err != nil {
		return err
	}
	if _, err := w.dst.Write(sealed); err != nil {
		return err
	}
	w.buf = w.buf[:0]
	return nil
}

// decryptArchiveTo entschlüsselt ein Archiv aus src und schreibt das enthaltene
// ZIP nach dst. Blockweise für LCMBAK3; die Altformate werden - mangels
// Alternative, sie sind ein einziges Siegel - als Ganzes gelesen.
func decryptArchiveTo(src io.Reader, dst io.Writer, passphrase string) error {
	magic := make([]byte, backupMagicLen)
	if _, err := io.ReadFull(src, magic); err != nil {
		return ErrBackupFormat
	}
	scN := scryptNForMagic(magic)
	if scN == 0 {
		return ErrBackupFormat
	}
	salt := make([]byte, backupSaltLen)
	if _, err := io.ReadFull(src, salt); err != nil {
		return ErrBackupFormat
	}
	aead, err := deriveAEAD(passphrase, salt, scN)
	if err != nil {
		return err
	}
	if bytes.Equal(magic, backupMagicV3) {
		return decryptChunked(src, dst, aead)
	}
	return decryptSealed(src, dst, aead)
}

// decryptChunked liest Block für Block und schreibt den Klartext weiter.
func decryptChunked(src io.Reader, dst io.Writer, aead cipher.AEAD) error {
	base := make([]byte, nonceBaseLen)
	if _, err := io.ReadFull(src, base); err != nil {
		return ErrBackupFormat
	}
	nonce := make([]byte, nonceBaseLen+4)
	copy(nonce, base)
	var (
		header [4]byte
		buf    []byte
		plain  []byte
		blocks uint32
	)
	for {
		if _, err := io.ReadFull(src, header[:]); err != nil {
			// Ein sauber beendetes Archiv endet mit dem Abschlussblock -
			// dort kehren wir zurück. Hier ist der Strom vorher zu Ende.
			return ErrBackupPassphrase
		}
		n := binary.BigEndian.Uint32(header[:])
		if n < uint32(aead.Overhead()) || n > maxChunkCipherLen {
			return ErrBackupPassphrase
		}
		if uint32(cap(buf)) < n {
			buf = make([]byte, n)
		}
		buf = buf[:n]
		if _, err := io.ReadFull(src, buf); err != nil {
			return ErrBackupPassphrase
		}
		binary.BigEndian.PutUint32(nonce[nonceBaseLen:], blocks)
		blocks++

		// Erst als Zwischenblock versuchen, dann als Abschlussblock: Welcher
		// es ist, steckt allein im AAD - und genau das macht das Abschneiden
		// eines Archivs erkennbar.
		out, err := aead.Open(plain[:0], nonce, buf, aadChunk)
		if err != nil {
			out, err = aead.Open(plain[:0], nonce, buf, aadFinal)
			if err != nil {
				return ErrBackupPassphrase
			}
			if len(out) > 0 {
				if _, err := dst.Write(out); err != nil {
					return err
				}
			}
			return nil
		}
		plain = out[:0]
		if _, err := dst.Write(out); err != nil {
			return err
		}
	}
}

// decryptSealed liest die Altformate (ein GCM-Siegel über dem ganzen Archiv).
func decryptSealed(src io.Reader, dst io.Writer, aead cipher.AEAD) error {
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(src, nonce); err != nil {
		return ErrBackupFormat
	}
	ciphertext, err := io.ReadAll(io.LimitReader(src, maxArchiveSize+1))
	if err != nil {
		return err
	}
	if int64(len(ciphertext)) > maxArchiveSize {
		return fmt.Errorf("archiv überschreitet das Größenlimit (%d MiB)", maxArchiveSize>>20)
	}
	plain, err := aead.Open(ciphertext[:0], nonce, ciphertext, nil)
	if err != nil {
		return ErrBackupPassphrase
	}
	_, err = dst.Write(plain)
	return err
}

// extractEncryptedArchive entschlüsselt ein Archiv und reicht jede enthaltene
// Datei einzeln als Datenstrom an fn weiter. Das entschlüsselte ZIP landet
// dabei in einer temporären Datei in tmpDir (ZIP braucht wahlfreien Zugriff)
// und nicht im Speicher.
func extractEncryptedArchive(src io.Reader, key archiveKey, tmpDir string, fn func(name string, r io.Reader) error) error {
	if key.empty() {
		return ErrBackupNoPassphrase
	}
	tmp, err := os.CreateTemp(tmpDir, ".lcmbak-*.zip")
	if err != nil {
		return fmt.Errorf("temporäre datei: %w", err)
	}
	defer func() {
		tmp.Close()
		os.Remove(tmp.Name())
	}()

	if err := decryptAnyArchiveTo(src, tmp, key); err != nil {
		return err
	}
	size, err := tmp.Seek(0, io.SeekEnd)
	if err != nil {
		return err
	}
	zr, err := zip.NewReader(tmp, size)
	if err != nil {
		return ErrBackupPassphrase
	}
	for _, zf := range zr.File {
		rc, err := zf.Open()
		if err != nil {
			return err
		}
		err = fn(zf.Name, rc)
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// decryptAnyArchiveTo erkennt das Format am Anfang der Datei und entschlüsselt
// in dst: age-Archive mit dem privaten Schlüssel, LCMBAK-Archive mit der
// Passphrase.
func decryptAnyArchiveTo(src io.Reader, dst io.Writer, key archiveKey) error {
	br := bufio.NewReaderSize(src, 64<<10)
	head, _ := br.Peek(4096) // Kennung und Kopf stehen ganz vorn
	if !bytes.HasPrefix(head, []byte(ageMagic)) {
		if key.passphrase == "" {
			return ErrBackupNoPassphrase
		}
		return decryptArchiveTo(br, dst, key.passphrase)
	}
	ids := key.identities
	if len(ids) == 0 {
		// Aus LCM gibt es kein age-Archiv mit Passphrase - aber ein von Hand
		// mit `age -p` erzeugtes; das erkennt man am scrypt-Eintrag im Kopf.
		if !bytes.Contains(head, []byte("\n-> scrypt ")) {
			return ErrBackupNoIdentity
		}
		id, err := age.NewScryptIdentity(key.passphrase)
		if err != nil {
			return ErrBackupPassphrase
		}
		ids = []age.Identity{id}
	}
	r, err := age.Decrypt(br, ids...)
	if err != nil {
		var noMatch *age.NoIdentityMatchError
		if errors.As(err, &noMatch) || errors.Is(err, age.ErrIncorrectIdentity) {
			return ErrBackupPassphrase
		}
		return ErrBackupFormat
	}
	if _, err := io.Copy(dst, r); err != nil {
		// Abgeschnitten oder manipuliert - age prüft jeden Block.
		return ErrBackupPassphrase
	}
	return nil
}

// buildEncryptedArchive packt Dateien aus dem Speicher in ein verschlüsseltes
// Archiv. Für kleine Inhalte und Tests; der Backup-Lauf selbst streamt
// (writeEncryptedArchive), damit die Datenbank nie am Stück im RAM liegt.
func buildEncryptedArchive(files []archiveFile, passphrase string) ([]byte, error) {
	sources := make([]archiveSource, 0, len(files))
	for _, f := range files {
		sources = append(sources, archiveSource{Name: f.Name, Data: f.Data})
	}
	var buf bytes.Buffer
	if err := writeEncryptedArchive(&buf, sources, passphrase); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// openEncryptedArchive entschlüsselt ein Archiv vollständig in den Speicher.
// Nur für kleine Archive und Tests - der Restore-Weg streamt
// (extractEncryptedArchive).
func openEncryptedArchive(data []byte, passphrase string) ([]archiveFile, error) {
	var files []archiveFile
	var total int64
	err := extractEncryptedArchive(bytes.NewReader(data), keyFromSecret(passphrase), "", func(name string, r io.Reader) error {
		content, err := io.ReadAll(io.LimitReader(r, maxArchiveSize-total+1))
		if err != nil {
			return err
		}
		total += int64(len(content))
		if total > maxArchiveSize {
			return fmt.Errorf("archiv überschreitet das Größenlimit (%d MiB)", maxArchiveSize>>20)
		}
		files = append(files, archiveFile{Name: name, Data: content})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

// deriveAEAD leitet den AES-256-GCM-AEAD aus Passphrase + Salt ab (scrypt).
// n ist der scrypt-Kostenparameter (aus dem Archiv-Magic bestimmt).
func deriveAEAD(passphrase string, salt []byte, n int) (cipher.AEAD, error) {
	key, err := scrypt.Key([]byte(passphrase), salt, n, scryptR, scryptP, scryptKeyLen)
	if err != nil {
		return nil, fmt.Errorf("schlüsselableitung: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
