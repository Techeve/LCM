package services

import (
	"errors"
	"fmt"
	"sync"

	"LCM/internal/core/domain"
	"LCM/internal/version"
)

// Warum ein Kanal gesperrt ist. Die Oberfläche formuliert daraus den Text.
const (
	ChannelLockBehind       = "behind"       // liegt bei Major- oder Feature-Nummer zurück
	ChannelLockUnknown      = "unknown"      // Stand nicht abrufbar
	ChannelLockSubscription = "subscription" // Enterprise ohne aktive Subscription
)

// ErrChannelBehind: Der Zielkanal liegt bei Major- oder Feature-Nummer hinter
// der installierten Version.
var ErrChannelBehind = errors.New("der Paketkanal liegt hinter der installierten Version")

// ErrChannelUnknown: Der Stand des Zielkanals ist nicht abrufbar.
var ErrChannelUnknown = errors.New("der Stand des Paketkanals ist nicht abrufbar")

// ChannelVersion ist der Stand eines Paketkanals aus Sicht dieses Hosts.
type ChannelVersion struct {
	Channel string `json:"channel"`
	// Latest ist die neueste lcm-Version im Kanal, leer wenn unbekannt.
	Latest string `json:"latest"`
	Error  string `json:"error,omitempty"`
	// Lock nennt den Grund, aus dem der Wechsel gesperrt ist; leer heißt frei.
	Lock string `json:"lock,omitempty"`
}

// ChannelsView ist die Antwort auf die Frage „wohin kann ich wechseln?".
type ChannelsView struct {
	Installed string           `json:"installed"`
	Channels  []ChannelVersion `json:"channels"`
}

// ChannelVersions fragt die neueste Version jedes Kanals ab und sagt, ob ein
// Wechsel dorthin erlaubt ist.
//
// Gesperrt ist ein Kanal, der bei Major- oder Feature-Nummer hinter der
// installierten Version liegt. apt spielt beim Wechsel nichts zurück, der Host
// bliebe also auf seiner Version stehen und bekäme aus dem neuen Kanal keine
// Updates mehr, bis der aufgeholt hat. Fehlt dabei ein Bugfix, ist das
// harmlos; fehlt eine Feature-Version, wäre der Host Wochen abgehängt. Der
// eigene Kanal bleibt immer wählbar, ein Wechsel nach vorn ebenso. Eine
// Entwicklungsversion (0.0.0-dev) liegt nie vorn, für sie ist jeder Wechsel frei.
func (s *SubscriptionService) ChannelVersions() (*ChannelsView, error) {
	settings, err := s.settings.Get()
	if err != nil {
		return nil, err
	}
	installed := version.Version
	current := settings.AptChannel()
	channels := []string{domain.AptChannelCommunity, domain.AptChannelBeta, domain.AptChannelEnterprise}
	view := &ChannelsView{Installed: installed, Channels: make([]ChannelVersion, len(channels))}

	var wg sync.WaitGroup
	for i, channel := range channels {
		wg.Add(1)
		go func() {
			defer wg.Done()
			view.Channels[i] = s.channelVersion(settings, channel, current, installed)
		}()
	}
	wg.Wait()
	return view, nil
}

func (s *SubscriptionService) channelVersion(settings *domain.GlobalSettings, channel, current, installed string) ChannelVersion {
	cv := ChannelVersion{Channel: channel}
	if channel == domain.AptChannelEnterprise && settings.SubscriptionStatus != "active" {
		cv.Lock = ChannelLockSubscription
		return cv
	}
	url, login, secret, err := channelIndexSource(settings, s.cipher, channel)
	if err == nil {
		cv.Latest, err = fetchLatestRepoVersion(url, login, secret)
	}
	if err != nil {
		cv.Error = err.Error()
	}
	if channel != current {
		cv.Lock = channelLock(cv.Latest, installed)
	}
	return cv
}

// channelLock entscheidet über einen Wechsel auf einen fremden Kanal.
// Ein Kanal, dessen Stand unbekannt ist, wird gesperrt: Ob er zurückliegt,
// lässt sich dann nicht sagen.
func channelLock(latest, installed string) string {
	switch {
	case latest == "":
		return ChannelLockUnknown
	case version.FeatureBehind(latest, installed):
		return ChannelLockBehind
	}
	return ""
}

// checkChannelSwitch lehnt einen Wechsel ab, der den Host abhängen würde. Die
// fehlende Subscription prüft SetAptChannel selbst, mit eigener Meldung.
func (s *SubscriptionService) checkChannelSwitch(settings *domain.GlobalSettings, channel string) error {
	if channel == settings.AptChannel() {
		return nil
	}
	installed := version.Version
	cv := s.channelVersion(settings, channel, settings.AptChannel(), installed)
	switch cv.Lock {
	case ChannelLockBehind:
		return fmt.Errorf("%w: %s hat %s, installiert ist %s", ErrChannelBehind, channel, cv.Latest, installed)
	case ChannelLockUnknown:
		return fmt.Errorf("%w: %s: %s", ErrChannelUnknown, channel, cv.Error)
	}
	return nil
}
