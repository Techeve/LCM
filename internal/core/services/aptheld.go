package services

import (
	"strings"

	"LCM/internal/core/domain"
	"LCM/internal/storage/repositories"
)

// Zurückgehaltene Updates (apt).
//
// Nicht jedes Paket, das `apt list --upgradable` meldet, kommt mit einem
// Upgrade auch wirklich mit. Drei Gründe gibt es, und apt nennt sie selbst im
// Probelauf (`apt-get -s --with-new-pkgs upgrade`):
//
//   - hold:   per `apt-mark hold` gesperrt - eine bewusste Entscheidung des
//     Betreibers. Erscheint bei apt unter „kept back".
//   - phased: gestaffelte Auslieferung (Ubuntu, Phased-Update-Percentage).
//     Ubuntu gibt ein Update erst einem Teil der Rechner frei, um Fehler früh
//     zu erkennen; der Rest bekommt es später automatisch.
//   - kept:   ließe sich nur einspielen, wenn andere Pakete entfernt würden
//     oder ein Konflikt aufgelöst wird - das bleibt dem Menschen überlassen.
//
// Solche Pakete zählen nicht als überfällige Updates (die Ampel straft sie
// nicht ab). Eine bekannte CVE darauf zählt trotzdem - über die CVE-Kriterien
// der Ampel, nicht über die Update-Zahl.

// aptHeldBackFunc definiert lcm_apt_heldback: Je zurückgehaltenem Paket eine
// Zeile "name grund". Der Probelauf braucht keine Root-Rechte und nimmt keine
// Sperre. LCM_APT_PHASED gilt auch hier, damit Probelauf und echter Lauf
// dieselbe Entscheidung treffen.
const aptHeldBackFunc = `lcm_apt_heldback() {
  __holds=" $(apt-mark showhold 2>/dev/null | tr '\n' ' ') "
  LC_ALL=C apt-get -s ${LCM_APT_PHASED:+-o APT::Get::Always-Include-Phased-Updates=true} --with-new-pkgs upgrade 2>/dev/null |
    awk '/have been kept back:/ {m = "kept"; next} /deferred due to phasing:/ {m = "phased"; next} /^[^ ]/ {m = ""} m != "" {for (i = 1; i <= NF; i++) print $i, m}' |
    while read -r __p __why; do
      case "$__holds" in *" $__p "*) __why=hold ;; esac
      echo "$__p $__why"
    done
}
`

// aptHeldBackReport definiert lcm_apt_report: die menschenlesbare Fassung von
// lcm_apt_heldback für das Job-Protokoll nach einem Upgrade.
const aptHeldBackReport = `lcm_apt_report() {
  __left=$(lcm_apt_heldback)
  [ -n "$__left" ] || return 0
  echo "LCM: $(printf '%s\n' "$__left" | wc -l | tr -d ' ') Paket(e) nicht aktualisiert:"
  printf '%s\n' "$__left" | while read -r __p __why; do
    case "$__why" in
      hold) __why="per apt-mark hold gesperrt" ;;
      phased) __why="gestaffelte Ubuntu-Auslieferung - kommt automatisch, sobald dieser Server an der Reihe ist" ;;
      *) __why="liesse sich nur durch Entfernen anderer Pakete einspielen - bitte von Hand pruefen (apt full-upgrade)" ;;
    esac
    echo "  $__p - $__why"
  done
}
`

// aptForceStep spielt gestaffelte Updates mit bekannter CVE gezielt ein
// (LCM_APT_FORCE, von aptUpgradeEnv gesetzt). Ein ausdrücklich genanntes
// Paket installiert apt ohne Rücksicht auf die Staffelung.
const aptForceStep = `if [ -n "${LCM_APT_FORCE:-}" ]; then
  echo "LCM: gestaffelte Updates mit bekannter CVE werden vorgezogen: $LCM_APT_FORCE"
  lcm_apt install --only-upgrade -y $LCM_APT_FORCE || rc=$?
fi`

// applyHeldBack trägt die Gründe aus lcm_apt_heldback am Paketbestand ein.
func applyHeldBack(pkgs []domain.Package, out string) {
	reasons := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		name, reason, ok := strings.Cut(strings.TrimSpace(line), " ")
		if ok && domain.ValidHeldReason(reason) {
			reasons[name] = reason
		}
	}
	for i := range pkgs {
		if pkgs[i].Outdated() {
			pkgs[i].HeldReason = reasons[pkgs[i].Name]
		}
	}
}

// aptUpgradeEnv baut die Shell-Variablen, mit denen ein Voll-Upgrade läuft:
// die globale Entscheidung über gestaffelte Updates und die Liste der
// gestaffelten Pakete, auf denen eine CVE liegt. Die Namen stammen aus dem
// Scan und werden trotzdem geprüft - sie landen unquotiert im Skript.
func aptUpgradeEnv(settings *repositories.SettingsRepository, servers *repositories.ServerRepository, server *domain.Server) string {
	var env strings.Builder
	if settings != nil {
		if st, err := settings.Get(); err == nil && st.AptIncludePhased {
			env.WriteString("LCM_APT_PHASED=1\n")
		}
	}
	names, err := servers.PhasedPackagesWithCVE(server.ID)
	if err != nil {
		return env.String()
	}
	var safe []string
	for _, n := range names {
		if rePackageName.MatchString(n) {
			safe = append(safe, n)
		}
	}
	if len(safe) > 0 {
		env.WriteString("LCM_APT_FORCE='" + strings.Join(safe, " ") + "'\n")
	}
	return env.String()
}
