package acmex

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/go-acme/lego/v5/challenge"
	"github.com/go-acme/lego/v5/challenge/dns01"
	"github.com/miekg/dns"
)

const (
	probePollInterval = 2 * time.Second
	probeQueryTimeout = 5 * time.Second
	probeCleanUpTime  = 30 * time.Second
)

// ProbeDNS prüft einen Anbieter ohne CA: Er setzt den TXT-Eintrag
// _acme-challenge.<name> mit einem Zufallswert, wartet bis zu wait darauf,
// dass die Resolver ihn liefern, und räumt ihn wieder auf. Das ist, was lego
// bei einer echten Prüfung tut, nur ohne dass die CA sie begleitet.
// checkNameservers wie bei DNS01; leer heißt die des Systems.
func ProbeDNS(ctx context.Context, p challenge.Provider, name string, checkNameservers []string, wait time.Duration) (err error) {
	useCheckNameservers(checkNameservers)
	domain := strings.TrimPrefix(name, "*.")
	token, keyAuth := randomHex(16), randomHex(32)
	info := dns01.GetChallengeInfo(ctx, domain, keyAuth)

	if err := p.Present(ctx, domain, token, keyAuth); err != nil {
		return fmt.Errorf("eintrag setzen: %w", err)
	}
	defer func() {
		// Auch nach Abbruch oder Zeitablauf des Aufrufs aufräumen.
		cleanCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), probeCleanUpTime)
		defer cancel()
		if cleanErr := p.CleanUp(cleanCtx, domain, token, keyAuth); cleanErr != nil {
			err = errors.Join(err, fmt.Errorf("eintrag entfernen: %w", cleanErr))
		}
	}()

	resolvers, err := probeResolvers(checkNameservers)
	if err != nil {
		return err
	}
	return waitForTXT(ctx, resolvers, info.EffectiveFQDN, info.Value, wait)
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b) // crypto/rand scheitert nicht
	return hex.EncodeToString(b)
}

// probeResolvers sind die Resolver, bei denen der Eintrag auftauchen soll.
func probeResolvers(checkNameservers []string) ([]string, error) {
	servers := checkNameservers
	if len(servers) == 0 {
		conf, err := dns.ClientConfigFromFile("/etc/resolv.conf")
		if err != nil || len(conf.Servers) == 0 {
			return nil, errors.New("keine resolver des systems gefunden - bitte prüf-nameserver eintragen")
		}
		servers = conf.Servers
		for i := range servers {
			servers[i] = net.JoinHostPort(servers[i], conf.Port)
		}
	}
	resolvers := make([]string, len(servers))
	for i, s := range servers {
		resolvers[i] = withPort(s)
	}
	return resolvers, nil
}

func withPort(server string) string {
	if _, _, err := net.SplitHostPort(server); err == nil {
		return server
	}
	return net.JoinHostPort(strings.Trim(server, "[]"), "53")
}

// waitForTXT fragt alle Resolver, bis jeder den Wert liefert oder die Zeit
// um ist. Der Fehler nennt, was zuletzt gefehlt hat.
func waitForTXT(ctx context.Context, resolvers []string, fqdn, value string, wait time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	var last error
	for {
		if last = allShowTXT(ctx, resolvers, fqdn, value); last == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("der eintrag wurde in %s nicht sichtbar: %w", wait, last)
		case <-time.After(probePollInterval):
		}
	}
}

func allShowTXT(ctx context.Context, resolvers []string, fqdn, value string) error {
	for _, resolver := range resolvers {
		if err := showsTXT(ctx, resolver, fqdn, value); err != nil {
			return fmt.Errorf("%s: %w", resolver, err)
		}
	}
	return nil
}

func showsTXT(ctx context.Context, resolver, fqdn, value string) error {
	query := new(dns.Msg).SetQuestion(dns.Fqdn(fqdn), dns.TypeTXT)
	client := &dns.Client{Timeout: probeQueryTimeout}
	reply, _, err := client.ExchangeContext(ctx, query, resolver)
	if err != nil {
		return err
	}
	for _, rr := range reply.Answer {
		if txt, ok := rr.(*dns.TXT); ok && strings.Join(txt.Txt, "") == value {
			return nil
		}
	}
	return fmt.Errorf("kein txt-eintrag mit dem erwarteten wert (antwort: %s)", dns.RcodeToString[reply.Rcode])
}
