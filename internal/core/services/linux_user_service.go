package services

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"LCM/internal/config"
	"LCM/internal/core/domain"
	"LCM/internal/infrastructure/crypto"
	"LCM/internal/infrastructure/sshx"
	"LCM/internal/storage/repositories"
)

var (
	ErrInvalidLinuxUsername    = errors.New("ungültiger linux-benutzername (a-z, 0-9, _, -; beginnt mit buchstabe/_)")
	ErrReservedLinuxUsername   = errors.New("dieser benutzername ist reserviert (root/lcm-svc/systemkonten) und kann nicht angelegt werden")
	ErrInvalidShell            = errors.New("ungültige shell (erwartet absoluter pfad, z.B. /bin/bash)")
	ErrLinuxUsernameTaken      = errors.New("linux-benutzername ist bereits vergeben")
	ErrInvalidSSHKey           = errors.New("ungültiger ssh-public-key (erwartet openssh-format)")
	ErrEmptyActivation         = errors.New("es muss ein passwort oder ein ssh-key gesetzt werden")
	ErrLinuxUserStillOnServers = errors.New("benutzer ist noch servern zugeordnet - erst von allen servern entfernen")
)

// ServerDeprovisionResult protokolliert das Deprovisionieren pro Server.
type ServerDeprovisionResult struct {
	ServerID uint   `json:"server_id"`
	Server   string `json:"server"`
	OK       bool   `json:"ok"`
	Message  string `json:"message"`
}

// LinuxUserService verwaltet den Katalog der Betriebssystem-Benutzer, deren
// SSH-Keys, Passwörter (Self-Service via Aktivierungslink) und die Zuordnung
// zu Servern/Gruppen. Die eigentliche Verteilung auf die Server erledigt der
// ProvisioningService.
type LinuxUserService struct {
	linux  *repositories.LinuxUserRepository
	groups *repositories.GroupRepository
	audit  *AuditService
	cipher *crypto.Cipher
	// prov stößt (optional) eine Neuverteilung an, wenn sich Keys oder
	// Credentials ändern.
	prov *ProvisioningService
	// profiles liefert die Berechtigungsprofile - gebraucht, um das
	// abgeleitete sudo-Bit aus dem Profil zu bestimmen. Optional.
	profiles *repositories.PrivilegeProfileRepository
}

// WithProfiles verdrahtet die Berechtigungsprofile.
func (s *LinuxUserService) WithProfiles(repo *repositories.PrivilegeProfileRepository) *LinuxUserService {
	s.profiles = repo
	return s
}

// applyDefaultProfile setzt das Standardprofil und leitet daraus das
// sudo-Bit ab. Maßgeblich ist das Profil; Sudo bleibt als abgeleiteter Wert
// erhalten, damit bestehende API-Clients weiterlaufen.
func (s *LinuxUserService) applyDefaultProfile(u *domain.LinuxUser, profileID *uint) error {
	u.DefaultProfileID = profileID
	if profileID == nil {
		u.Sudo = false
		return nil
	}
	if s.profiles == nil {
		return nil
	}
	profile, err := s.profiles.FindByID(*profileID)
	if err != nil {
		return err
	}
	u.Sudo = profile.GrantsFullRoot
	return nil
}

// builtinProfileID liefert die ID eines mitgelieferten Profils - der
// Übersetzungsweg für Clients, die weiterhin nur „sudo: true/false" kennen.
func (s *LinuxUserService) builtinProfileID(fullRoot bool) *uint {
	if s.profiles == nil {
		return nil
	}
	slug := domain.ProfileSlugStandard
	if fullRoot {
		slug = domain.ProfileSlugFullAdmin
	}
	profile, err := s.profiles.FindBySlug(slug)
	if err != nil {
		return nil
	}
	return &profile.ID
}

func NewLinuxUserService(linux *repositories.LinuxUserRepository, groups *repositories.GroupRepository, audit *AuditService, cipher *crypto.Cipher, prov *ProvisioningService) *LinuxUserService {
	return &LinuxUserService{linux: linux, groups: groups, audit: audit, cipher: cipher, prov: prov}
}

func (s *LinuxUserService) List() ([]domain.LinuxUser, error) {
	users, err := s.linux.FindAll()
	if err != nil {
		return nil, err
	}
	for i := range users {
		users[i].HasPassword = users[i].PasswordEnc != ""
	}
	return users, nil
}

func (s *LinuxUserService) Get(id uint) (*domain.LinuxUser, error) {
	u, err := s.linux.FindByID(id)
	if err != nil {
		return nil, err
	}
	u.HasPassword = u.PasswordEnc != ""
	return u, nil
}

// LinuxUserCreateInput sind die Angaben zum Anlegen - wie bei Update eine
// Struktur statt einer wachsenden Argumentliste.
type LinuxUserCreateInput struct {
	Username string
	FullName string
	Email    string
	Shell    string
	// Sudo ist der alte Schalter; er waehlt eines der mitgelieferten Profile.
	Sudo bool
	// DefaultProfileID hat Vorrang vor Sudo. Fehlte hier bis 1.26: Wer ueber
	// die API anlegte, bekam kommentarlos das Standardprofil, obwohl er ein
	// anderes mitgeschickt hatte (Etappe G, 21.08.2026).
	DefaultProfileID *uint
}

// Create legt einen neuen Linux-Benutzer an.
func (s *LinuxUserService) Create(in LinuxUserCreateInput, actor string) (*domain.LinuxUser, error) {
	username, fullName, email, shell, sudo := in.Username, in.FullName, in.Email, in.Shell, in.Sudo
	username = strings.ToLower(strings.TrimSpace(username))
	if !domain.ValidLinuxUsername(username) {
		return nil, ErrInvalidLinuxUsername
	}
	if domain.IsReservedLinuxUsername(username) {
		return nil, ErrReservedLinuxUsername
	}
	if _, err := s.linux.FindByUsername(username); err == nil {
		return nil, ErrLinuxUsernameTaken
	} else if !errors.Is(err, repositories.ErrNotFound) {
		return nil, err
	}
	if shell == "" {
		shell = "/bin/bash"
	}
	if !domain.ValidLinuxShell(shell) {
		return nil, ErrInvalidShell
	}
	u := &domain.LinuxUser{Username: username, FullName: fullName, Email: email, Shell: shell, Active: true}
	profil := in.DefaultProfileID
	if profil == nil {
		profil = s.builtinProfileID(sudo)
	}
	if err := s.applyDefaultProfile(u, profil); err != nil {
		return nil, err
	}
	if err := s.linux.Create(u); err != nil {
		return nil, err
	}
	s.audit.Log(actor, "linuxuser.create", "linux_user", u.ID, username)
	return u, nil
}

// Update ändert Anzeigename, E-Mail, Shell, sudo-Flag und Aktiv-Status.
// LinuxUserUpdateInput ist der PATCH-Rumpf: nil = Feld nicht mitgeschickt =
// unverändert. Früher wirkte der Endpunkt wie ein PUT - ein Sudo-Grant
// ({"sudo":true}) setzte nebenbei active=false und leerte Name/E-Mail; der
// Benutzer stand dann als deaktiviert im Katalog, blieb aber auf den
// Servern voll berechtigt (R2-041).
type LinuxUserUpdateInput struct {
	FullName *string
	Email    *string
	Shell    *string
	Sudo     *bool
	Active   *bool
	// DefaultProfileID setzt das Berechtigungsprofil. Hat Vorrang vor Sudo:
	// Wer das Profil angibt, meint das Profil.
	DefaultProfileID *uint
}

func (s *LinuxUserService) Update(id uint, in LinuxUserUpdateInput, actor string) (*domain.LinuxUser, error) {
	u, err := s.linux.FindByID(id)
	if err != nil {
		return nil, err
	}
	var changed []string
	if in.FullName != nil {
		u.FullName = *in.FullName
		changed = append(changed, "full_name")
	}
	if in.Email != nil {
		u.Email = *in.Email
		changed = append(changed, "email")
	}
	if in.Shell != nil && *in.Shell != "" {
		if !domain.ValidLinuxShell(*in.Shell) {
			return nil, ErrInvalidShell
		}
		u.Shell = *in.Shell
		changed = append(changed, "shell")
	}
	switch {
	case in.DefaultProfileID != nil:
		if err := s.applyDefaultProfile(u, in.DefaultProfileID); err != nil {
			return nil, err
		}
		changed = append(changed, "profil")
	case in.Sudo != nil:
		// Alt-Clients kennen nur das Häkchen - es wird auf das passende
		// mitgelieferte Profil abgebildet.
		if err := s.applyDefaultProfile(u, s.builtinProfileID(*in.Sudo)); err != nil {
			return nil, err
		}
		changed = append(changed, "sudo")
	}
	if in.Active != nil {
		u.Active = *in.Active
		changed = append(changed, "active")
	}
	if err := s.linux.Update(u); err != nil {
		return nil, err
	}
	s.audit.Log(actor, "linuxuser.update", "linux_user", id, u.Username+" - Felder: "+strings.Join(changed, ", "))
	s.syncUserEverywhere(id, actor)
	return s.Get(id)
}

// RemoveFromAllServers deprovisioniert den Benutzer AKTIV von allen Servern,
// auf denen er (direkt oder über eine Gruppe) provisioniert ist - der Account
// wird dort per userdel entfernt - und löst danach sämtliche Zuordnungen.
// Erst danach ist der Benutzer löschbar. Liefert einen Bericht pro Server.
func (s *LinuxUserService) RemoveFromAllServers(id uint, actor string) ([]ServerDeprovisionResult, error) {
	return s.RemoveFromServers(id, nil, actor)
}

// RemoveFromServers deprovisioniert den Benutzer von den GENANNTEN Servern
// (serverIDs leer = alle). Früher wurde das Feld server_ids still ignoriert
// und IMMER überall deprovisioniert - wer einen Benutzer von einem System
// abziehen wollte, nahm ihm den Zugang auf allen (R2-042). Nur direkt
// zugeordnete Server lassen sich einzeln lösen; eine Gruppen-Zuordnung wird
// mit Klartext abgelehnt (sonst provisionierte der nächste Gruppen-Sync das
// Konto sofort wieder - die Entfernung wäre eine Scheinwirkung).
func (s *LinuxUserService) RemoveFromServers(id uint, serverIDs []uint, actor string) ([]ServerDeprovisionResult, error) {
	u, err := s.linux.FindByID(id)
	if err != nil {
		return nil, err
	}
	servers, err := s.linux.ServersForUser(id)
	if err != nil {
		return nil, err
	}
	partial := len(serverIDs) > 0
	if partial {
		byID := map[uint]int{}
		for i := range servers {
			byID[servers[i].ID] = i
		}
		var selected []domain.Server
		var results []ServerDeprovisionResult
		for _, sid := range serverIDs {
			idx, ok := byID[sid]
			if !ok {
				// Unbekannte/nicht zugeordnete ID: benennen statt still
				// überspringen - der Aufrufer soll die Diskrepanz sehen.
				results = append(results, ServerDeprovisionResult{
					ServerID: sid, OK: false,
					Message: "diesem Benutzer nicht zugeordnet - nichts entfernt",
				})
				continue
			}
			direct, derr := s.linux.DirectlyAssigned(id, sid)
			if derr != nil {
				return nil, derr
			}
			if !direct {
				results = append(results, ServerDeprovisionResult{
					ServerID: sid, Server: servers[idx].Name, OK: false,
					Message: "über eine Gruppe zugeordnet - erst aus der Gruppe nehmen, sonst provisioniert der nächste Sync das Konto sofort wieder",
				})
				continue
			}
			selected = append(selected, servers[idx])
		}
		for i := range selected {
			srv := &selected[i]
			res := ServerDeprovisionResult{ServerID: srv.ID, Server: srv.Name, OK: true, Message: "entfernt"}
			if s.prov != nil {
				if _, err := s.prov.DeprovisionUser(srv, u.Username, actor); err != nil {
					res.OK = false
					res.Message = err.Error()
				}
			}
			if res.OK {
				if err := s.linux.RemoveFromServer(id, srv.ID); err != nil {
					return results, err
				}
			}
			results = append(results, res)
		}
		s.audit.Log(actor, "linuxuser.remove-from-servers", "linux_user", id,
			fmt.Sprintf("%s - %d Server angefragt", u.Username, len(serverIDs)))
		return results, nil
	}

	results := make([]ServerDeprovisionResult, 0, len(servers))
	allOK := true
	for i := range servers {
		srv := &servers[i]
		res := ServerDeprovisionResult{ServerID: srv.ID, Server: srv.Name, OK: true, Message: "entfernt"}
		if s.prov != nil {
			if _, err := s.prov.DeprovisionUser(srv, u.Username, actor); err != nil {
				res.OK = false
				res.Message = err.Error()
				allOK = false
			}
		}
		results = append(results, res)
	}
	// Zuordnungen NUR lösen, wenn wirklich überall entfernt wurde. Vorher
	// wurden sie auch bei Fehlschlägen gelöscht - das Konto blieb auf dem
	// Zielsystem nutzbar, während LCM es nirgends mehr führte: ein
	// verwaister Zugang, als „entfernt" ausgewiesen (R2-040). Bleibt die
	// Zuordnung bestehen, sieht der Betreiber den Fehlschlag, kann die
	// Ursache beheben und erneut entfernen; der Benutzer ist derweil
	// weiterhin sichtbar und verwaltet.
	if !allOK {
		s.audit.Log(actor, "linuxuser.remove-from-all-servers", "linux_user", id,
			u.Username+" - unvollständig, Zuordnungen bleiben bestehen")
		return results, nil
	}
	if err := s.linux.ClearAssignments(id); err != nil {
		return results, err
	}
	s.audit.Log(actor, "linuxuser.remove-from-all-servers", "linux_user", id, u.Username)
	return results, nil
}

// Delete löscht den Benutzer aus LCM - aber nur, wenn er auf KEINEM Server
// mehr provisioniert ist. Sonst muss zuerst RemoveFromAllServers laufen
// (Sicherheitsvorgabe: keine verwaisten Accounts auf den Zielsystemen).
func (s *LinuxUserService) Delete(id uint, actor string) error {
	u, err := s.linux.FindByID(id)
	if err != nil {
		return err
	}
	servers, err := s.linux.ServersForUser(id)
	if err != nil {
		return err
	}
	if len(servers) > 0 {
		return ErrLinuxUserStillOnServers
	}
	if err := s.linux.Delete(id); err != nil {
		return err
	}
	s.audit.Log(actor, "linuxuser.delete", "linux_user", id, u.Username)
	return nil
}

// ---- SSH-Keys ----------------------------------------------------------------

func (s *LinuxUserService) AddSSHKey(linuxUserID uint, name, publicKey, actor string) (*domain.LinuxUserSSHKey, error) {
	publicKey = strings.TrimSpace(publicKey)
	if !validSSHPublicKey(publicKey) {
		return nil, ErrInvalidSSHKey
	}
	if _, err := s.linux.FindByID(linuxUserID); err != nil {
		return nil, err
	}
	key := &domain.LinuxUserSSHKey{LinuxUserID: linuxUserID, Name: name, PublicKey: publicKey}
	if err := s.linux.AddSSHKey(key); err != nil {
		return nil, err
	}
	s.audit.Log(actor, "linuxuser.add-key", "linux_user", linuxUserID, name)
	s.syncUserEverywhere(linuxUserID, actor)
	return key, nil
}

// GenerateSSHKey erzeugt serverseitig ein ed25519-Schlüsselpaar für den
// Linux-Benutzer: Der Public Key wird gespeichert, der private Schlüssel
// wird GENAU EINMAL zurückgegeben und nirgends persistiert - der Anwender
// lädt ihn direkt herunter.
func (s *LinuxUserService) GenerateSSHKey(linuxUserID uint, name, actor string) (*domain.LinuxUserSSHKey, string, error) {
	u, err := s.linux.FindByID(linuxUserID)
	if err != nil {
		return nil, "", err
	}
	if strings.TrimSpace(name) == "" {
		name = "Generiert"
	}
	privPEM, pubLine, err := sshx.GenerateKeyPair(u.Username + "@lcm")
	if err != nil {
		return nil, "", err
	}
	key := &domain.LinuxUserSSHKey{LinuxUserID: linuxUserID, Name: name, PublicKey: pubLine}
	if err := s.linux.AddSSHKey(key); err != nil {
		return nil, "", err
	}
	s.audit.Log(actor, "linuxuser.generate-key", "linux_user", linuxUserID, name)
	s.syncUserEverywhere(linuxUserID, actor)
	return key, privPEM, nil
}

func (s *LinuxUserService) RemoveSSHKey(id uint, actor string) error {
	// Vor dem Löschen merken, wem der Schlüssel gehört - danach ist die
	// Zuordnung weg, und der Abgleich wüsste nicht mehr, welche Server der
	// entzogene Schlüssel verlassen muss.
	owner, err := s.linux.FindSSHKeyOwner(id)
	if err != nil {
		return err
	}
	if err := s.linux.DeleteSSHKey(id); err != nil {
		return err
	}
	s.audit.Log(actor, "linuxuser.remove-key", "ssh_key", id, "")
	s.syncUserEverywhere(owner, actor)
	return nil
}

// ---- Zuordnung zu Servergruppen ---------------------------------------------

// AssignToGroup ordnet den Linux-User einer Gruppe zu und richtet ihn auf
// deren Servern ein. Nicht erreichbare Server holen das nach (Rückstand).
// profileID ist optional: nil = das Standardprofil des Benutzers gilt auch
// auf den Servern dieser Gruppe.
func (s *LinuxUserService) AssignToGroup(scope repositories.AccessScope, linuxUserID, groupID uint, profileID *uint, actor string) error {
	group, err := s.groups.FindByID(scope, groupID)
	if err != nil {
		return err
	}
	if _, err := s.linux.FindByID(linuxUserID); err != nil {
		return err
	}
	if err := s.linux.AssignToGroup(linuxUserID, groupID); err != nil {
		return err
	}
	if err := s.linux.SetGroupAssignmentProfile(linuxUserID, groupID, profileID); err != nil {
		return err
	}
	s.audit.Log(actor, "linuxuser.assign-group", "linux_user", linuxUserID, s.assignmentLabel(group.Name, profileID))
	s.reconcileGroup(group.ID, nil, actor)
	return nil
}

// SetGroupProfile ändert das Profil einer bestehenden Gruppen-Zuweisung und
// gleicht die Server der Gruppe ab.
func (s *LinuxUserService) SetGroupProfile(scope repositories.AccessScope, linuxUserID, groupID uint, profileID *uint, actor string) error {
	group, err := s.groups.FindByID(scope, groupID)
	if err != nil {
		return err
	}
	if err := s.linux.SetGroupAssignmentProfile(linuxUserID, groupID, profileID); err != nil {
		return err
	}
	s.audit.Log(actor, "linuxuser.assign-profile", "linux_user", linuxUserID, s.assignmentLabel(group.Name, profileID))
	s.reconcileGroup(group.ID, nil, actor)
	return nil
}

// assignmentLabel beschreibt eine Zuweisung fürs Audit-Log. Ein Profil vergibt
// Root-nahe Rechte - welches wo gilt, gehört in die manipulationssichere Kette.
func (s *LinuxUserService) assignmentLabel(groupName string, profileID *uint) string {
	if profileID == nil {
		return groupName + " - Standardprofil des Benutzers"
	}
	if s.profiles == nil {
		return groupName
	}
	profile, err := s.profiles.FindByID(*profileID)
	if err != nil {
		return groupName
	}
	return groupName + " - Profil " + profile.Name
}

// GroupAssignments liefert die Gruppen-Zuweisungen eines Benutzers samt
// gesetztem Profil.
func (s *LinuxUserService) GroupAssignments(linuxUserID uint) ([]domain.ServerGroupLinuxUser, error) {
	return s.linux.GroupAssignments(linuxUserID)
}

// RemoveFromGroup löst die Zuordnung und entfernt das Konto von den Servern
// der Gruppe - außer der Benutzer ist dort direkt oder über eine andere
// Gruppe weiterhin berechtigt.
func (s *LinuxUserService) RemoveFromGroup(scope repositories.AccessScope, linuxUserID, groupID uint, actor string) error {
	group, err := s.groups.FindByID(scope, groupID)
	if err != nil {
		return err
	}
	before := s.entitledPerServer(group.ID)
	if err := s.linux.RemoveFromGroup(linuxUserID, groupID); err != nil {
		return err
	}
	s.audit.Log(actor, "linuxuser.remove-group", "linux_user", linuxUserID, "")
	s.reconcileGroup(group.ID, before, actor)
	return nil
}

// ---- Automatischer Abgleich -------------------------------------------------

// syncUserEverywhere bringt alle Server auf den Stand, auf denen der Benutzer
// eingerichtet ist - nach einer Änderung an Konto, Schlüsseln oder Passwort.
func (s *LinuxUserService) syncUserEverywhere(linuxUserID uint, actor string) {
	if s.prov == nil {
		return
	}
	servers, err := s.linux.ServersForUser(linuxUserID)
	if err != nil {
		slog.Error("user sync: servers of the account not readable", "user", linuxUserID, "error", err)
		return
	}
	s.prov.ReconcileServers(servers, nil, actor)
}

// entitledPerServer erhebt je Server der Gruppe die aktuell berechtigten
// Konten - die Aufnahme VOR einer Änderung, gegen die danach verglichen wird.
func (s *LinuxUserService) entitledPerServer(groupID uint) map[uint][]string {
	if s.prov == nil {
		return nil
	}
	servers, err := s.groups.ServersOfGroup(groupID)
	if err != nil {
		slog.Error("user sync: group members not readable", "group", groupID, "error", err)
		return nil
	}
	before := make(map[uint][]string, len(servers))
	for i := range servers {
		before[servers[i].ID] = s.prov.EntitledUsernames(servers[i].ID)
	}
	return before
}

// reconcileGroup gleicht alle Server einer Gruppe ab.
func (s *LinuxUserService) reconcileGroup(groupID uint, before map[uint][]string, actor string) {
	if s.prov == nil {
		return
	}
	servers, err := s.groups.ServersOfGroup(groupID)
	if err != nil {
		slog.Error("user sync: group members not readable", "group", groupID, "error", err)
		return
	}
	s.prov.ReconcileServers(servers, before, actor)
}

// ---- Aktivierungslinks (Self-Service Credentials) ---------------------------

// GenerateActivation erstellt einen zeitlich begrenzten Aktivierungslink,
// über den der Mitarbeiter selbst Passwort und/oder SSH-Key setzt. Liefert
// das Klartext-Token (einmalig - nur der Hash wird gespeichert).
func (s *LinuxUserService) GenerateActivation(id uint, ttl time.Duration, actor string) (string, *domain.LinuxUserActivation, error) {
	if _, err := s.linux.FindByID(id); err != nil {
		return "", nil, err
	}
	// TTL begrenzen: 0/negativ → Standard; über der Obergrenze → Obergrenze.
	// Vorher gab es KEINE Obergrenze - ein Link mit Ablauf im Jahr 2140 war
	// möglich (R2-044).
	ttl = clampActivationTTL(ttl)
	token := config.RandomSecret(32)
	act := &domain.LinuxUserActivation{
		LinuxUserID: id,
		TokenHash:   hashToken(token),
		ExpiresAt:   time.Now().Add(ttl),
	}
	if err := s.linux.CreateActivation(act); err != nil {
		return "", nil, err
	}
	s.audit.Log(actor, "linuxuser.activation-link", "linux_user", id, "")
	return token, act, nil
}

// LinuxActivationInput sind die vom Mitarbeiter gesetzten Credentials.
type LinuxActivationInput struct {
	Password  string // optional
	KeyName   string
	PublicKey string // optional
	// GenerateKey: statt einen eigenen Public Key einzureichen, erzeugt LCM
	// das Schlüsselpaar - der private Schlüssel geht einmalig an den
	// Mitarbeiter zurück (Download) und wird nie gespeichert.
	GenerateKey bool
}

// ConsumeActivation löst einen Aktivierungslink ein: setzt Passwort und/oder
// SSH-Key des Linux-Benutzers. Öffentlicher Endpunkt (kein Login nötig).
// Bei GenerateKey enthält der zweite Rückgabewert den privaten Schlüssel
// (einmalig, wird nicht persistiert), sonst ist er leer.
func (s *LinuxUserService) ConsumeActivation(token string, in LinuxActivationInput) (*domain.LinuxUser, string, error) {
	in.Password = strings.TrimSpace(in.Password)
	in.PublicKey = strings.TrimSpace(in.PublicKey)
	if in.Password == "" && in.PublicKey == "" && !in.GenerateKey {
		return nil, "", ErrEmptyActivation
	}
	if in.PublicKey != "" && !in.GenerateKey && !validSSHPublicKey(in.PublicKey) {
		return nil, "", ErrInvalidSSHKey
	}

	act, err := s.linux.FindActivationByTokenHash(hashToken(token))
	if err != nil {
		if errors.Is(err, repositories.ErrNotFound) {
			return nil, "", ErrActivationInvalid
		}
		return nil, "", err
	}
	if !act.Usable(time.Now()) {
		return nil, "", ErrActivationExpired
	}

	// Passwort-Policy auch hier - dieser Endpunkt ist ÖFFENTLICH und setzt
	// per chpasswd ein Login-Passwort auf allen zugeordneten Servern. Früher
	// prüfte er nur die Länge und ließ damit „1234567890" durch, während alle
	// anderen Pfade die volle Policy erzwangen. Die Prüfung läuft NACH der
	// Token-Prüfung, damit die Fehlermeldung nicht verrät, ob ein Token gültig
	// ist. Der Kontoinhaber ist über die Aktivierung bekannt - so greifen auch
	// die personenbezogenen Regeln.
	if in.Password != "" {
		owner, err := s.linux.FindByID(act.LinuxUserID)
		if err != nil {
			return nil, "", err
		}
		if err := EnforcePasswordPolicy(in.Password, PasswordIdentity{
			Username: owner.Username, Email: owner.Email, FirstName: owner.FullName,
		}); err != nil {
			return nil, "", err
		}
	}

	if in.Password != "" {
		enc, err := s.cipher.EncryptString(in.Password)
		if err != nil {
			return nil, "", err
		}
		if err := s.linux.UpdateFields(act.LinuxUserID, map[string]any{"password_enc": enc}); err != nil {
			return nil, "", err
		}
	}
	name := in.KeyName
	if name == "" {
		name = "Aktivierung"
	}
	privateKey := ""
	if in.GenerateKey {
		// Schlüsselpaar serverseitig erzeugen; überschreibt einen ggf.
		// mitgeschickten Public Key (die UI bietet nur eines von beidem an).
		u, err := s.linux.FindByID(act.LinuxUserID)
		if err != nil {
			return nil, "", err
		}
		privPEM, pubLine, err := sshx.GenerateKeyPair(u.Username + "@lcm")
		if err != nil {
			return nil, "", err
		}
		privateKey = privPEM
		in.PublicKey = pubLine
	}
	if in.PublicKey != "" {
		if err := s.linux.AddSSHKey(&domain.LinuxUserSSHKey{
			LinuxUserID: act.LinuxUserID, Name: name, PublicKey: in.PublicKey,
		}); err != nil {
			return nil, "", err
		}
	}

	now := time.Now()
	act.ConsumedAt = &now
	if err := s.linux.MarkActivationConsumed(act); err != nil {
		return nil, "", err
	}
	s.audit.Log("self-service", "linuxuser.activate", "linux_user", act.LinuxUserID, "")
	s.syncUserEverywhere(act.LinuxUserID, "self-service")
	u, err := s.Get(act.LinuxUserID)
	return u, privateKey, err
}

// validSSHPublicKey prüft grob das OpenSSH-Format (Zero-Bloat, kein
// vollständiges Parsen): erwartet "ssh-...|ecdsa-...|sk-... base64...".
func validSSHPublicKey(key string) bool {
	parts := strings.Fields(key)
	if len(parts) < 2 {
		return false
	}
	prefixes := []string{"ssh-rsa", "ssh-ed25519", "ssh-dss", "ecdsa-sha2-", "sk-ssh-", "sk-ecdsa-"}
	for _, p := range prefixes {
		if strings.HasPrefix(parts[0], p) {
			return len(parts[1]) > 20
		}
	}
	return false
}
