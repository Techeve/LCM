package services

import (
	"log/slog"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

// Das Selbst-Update: LCM verwaltet auch den Rechner, auf dem es läuft. Wer
// dort Pakete aktualisiert, aktualisiert damit früher oder später LCM selbst -
// und das Paket nimmt den Dienst mit, denn nach der Installation startet
// systemd ihn neu. Die Goroutine des laufenden Jobs stirbt mitten im Lauf,
// noch bevor sie ihr Ergebnis schreiben kann.
//
// Beim nächsten Start fand das Aufräumen dann einen Job vor, der auf
// „running" stand, und meldete ihn als Fehler - obwohl er das Gegenteil ist:
// Der Dienst läuft ja gerade deshalb in einer neuen Version. Der Fehler war
// also nicht der Job, sondern der Schluss aus seinem Zustand.
//
// Erkennen lässt sich der Fall am Zusammentreffen dreier Umstände, die sonst
// nicht zusammenkommen: Der Start läuft in einer anderen Version als der
// vorige, der offene Job spielte Pakete ein, und er lief auf dem LCM-Host.

// selfUpdateJobTypes sind die Job-Arten, die Pakete einspielen und damit das
// LCM-Paket selbst mitnehmen können. Der Paket-Scan gehört nicht dazu - er
// installiert nichts.
var selfUpdateJobTypes = map[string]bool{
	domain.RuleTypeUpdate:   true,
	domain.RuleTypePackages: true,
	domain.RuleTypeSecurity: true,
}

// LcmHostServer liefert den selbst-registrierten LCM-Host aus der Verwaltung
// (nil = nicht vorhanden). Das ist der Server, auf dem LCM selbst läuft.
func LcmHostServer(servers *repositories.ServerRepository) *domain.Server {
	all, err := servers.FindAll(repositories.ScopeAll())
	if err != nil {
		slog.Error("lcm host could not be determined", "error", err)
		return nil
	}
	for i := range all {
		if all[i].IsLcmHost() {
			return &all[i]
		}
	}
	return nil
}

// SelfUpdateOnRestart baut die Sonderbehandlung für
// FailInterruptedOnStartup. Sie liefert nil, wenn dieser Start kein
// Versionswechsel war oder der LCM-Host nicht in der Verwaltung steht - dann
// bleibt es beim Regelfall, jeder verwaiste Job ist ein Fehler.
//
// previous ist die zuletzt gelaufene Version (leer bei Erstinstallation),
// current die des laufenden Binaries.
func SelfUpdateOnRestart(servers *repositories.ServerRepository, previous, current string) *SelfUpdateRecovery {
	if previous == "" || previous == current {
		return nil
	}
	host := LcmHostServer(servers)
	if host == nil {
		return nil
	}
	hostID := host.ID
	return &SelfUpdateRecovery{
		Match: func(job *domain.Job) bool {
			return job.ServerID != nil && *job.ServerID == hostID && selfUpdateJobTypes[job.Type]
		},
		Note: "Mit diesem Lauf hat sich LCM selbst aktualisiert (" + previous + " → " + current +
			") und dabei neu gestartet. Der Abbruch der Verbindung gehört dazu - das Update ist eingespielt.",
	}
}
