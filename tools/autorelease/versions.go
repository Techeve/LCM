package main

import (
	"fmt"
	"strconv"
	"strings"
)

// version ist eine SemVer-Fassung ohne Build-Metadaten.
type version struct {
	core [3]int
	pre  string // "beta.2" oder ""
}

func parseVersion(s string) (version, error) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	s, _, _ = strings.Cut(s, "+") // Build-Metadaten wie +incompatible zählen nicht
	main, pre, _ := strings.Cut(s, "-")
	parts := strings.Split(main, ".")
	if len(parts) != 3 {
		return version{}, fmt.Errorf("keine gültige Version: %q", s)
	}
	v := version{pre: pre}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return version{}, fmt.Errorf("keine gültige Version: %q", s)
		}
		v.core[i] = n
	}
	return v, nil
}

// compareVersions liefert -1, 0 oder 1. Nach SemVer liegt eine Vorabversion
// unter der Fassung ohne Suffix (2.4.0-beta.9 < 2.4.0).
func compareVersions(a, b string) (int, error) {
	va, err := parseVersion(a)
	if err != nil {
		return 0, err
	}
	vb, err := parseVersion(b)
	if err != nil {
		return 0, err
	}
	for i := range va.core {
		if va.core[i] != vb.core[i] {
			return sign(va.core[i] - vb.core[i]), nil
		}
	}
	switch {
	case va.pre == vb.pre:
		return 0, nil
	case va.pre == "":
		return 1, nil
	case vb.pre == "":
		return -1, nil
	}
	return comparePre(va.pre, vb.pre), nil
}

// comparePre vergleicht Vorabversions-Kennungen punktweise: Zahlen numerisch
// und unter Text, Text nach Zeichen, die kürzere Liste zuerst.
func comparePre(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) && i < len(pb); i++ {
		na, errA := strconv.Atoi(pa[i])
		nb, errB := strconv.Atoi(pb[i])
		switch {
		case errA == nil && errB == nil && na != nb:
			return sign(na - nb)
		case errA == nil && errB != nil:
			return -1
		case errA != nil && errB == nil:
			return 1
		case errA != nil && errB != nil && pa[i] != pb[i]:
			return sign(strings.Compare(pa[i], pb[i]))
		}
	}
	return sign(len(pa) - len(pb))
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	}
	return 0
}

// nextFreePatch liefert die nächste Patch-Nummer der Linie, die kein Tag
// belegt. Gezählt wird über ALLE Tags des Repositorys, nicht nur die der
// Linie: eine einmal vergebene Nummer wird nie wieder verwendet, auch nicht
// als Vorabversion (v2.2.4-beta.1 sperrt 2.2.4).
func nextFreePatch(lineVersion string, tags []string) (string, error) {
	v, err := parseVersion(lineVersion)
	if err != nil {
		return "", err
	}
	for patch := v.core[2] + 1; ; patch++ {
		candidate := fmt.Sprintf("%d.%d.%d", v.core[0], v.core[1], patch)
		if !tagTaken(candidate, tags) {
			return candidate, nil
		}
	}
}

func tagTaken(version string, tags []string) bool {
	for _, tag := range tags {
		if tag == "v"+version || strings.HasPrefix(tag, "v"+version+"-") {
			return true
		}
	}
	return false
}
