package acmex

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"time"

	"github.com/go-acme/lego/v5/acme/api"
	"github.com/go-acme/lego/v5/certcrypto"
	"github.com/go-acme/lego/v5/certificate"
	"github.com/go-acme/lego/v5/challenge"
	"github.com/go-acme/lego/v5/challenge/dns01"
	"github.com/go-acme/lego/v5/challenge/resolver"
	"github.com/go-acme/lego/v5/lego"
)

// Config sagt, was LCM bei der CA ausstellen soll und wie es sich ausweist.
type Config struct {
	DirectoryURL string
	Email        string // Kontakt, darf leer sein
	Names        []string
	// PerName stellt je Name ein eigenes Zertifikat aus, sonst eines mit
	// allen Namen.
	PerName bool
	// TermsAccepted: Der Admin hat die Nutzungsbedingungen dieser CA
	// angenommen. Nur dann legt LCM ein Konto an.
	TermsAccepted bool
	// Solver richtet die Challenge ein, mit der die CA die Namen prüft.
	Solver Solver
}

// Provider legt die Antwort auf eine Prüfung der CA aus und räumt sie wieder
// ab (lego: challenge.Provider).
type Provider = challenge.Provider

// Solver richtet einen Challenge-Typ im lego-Client ein. HTTP-01 liefert
// HTTP01; ein weiterer Typ ist ein weiterer Solver.
type Solver func(*resolver.SolverManager) error

// DNS01 beantwortet die Prüfung der CA über einen DNS-Anbieter, der den
// TXT-Eintrag setzt und wieder entfernt. checkNameservers sind die Resolver,
// über die lego den Eintrag prüft, bevor es die CA fragen lässt; leer heißt
// die des Systems.
//
// lego hält diese Resolver für den ganzen Prozess (dns01.DefaultClient). LCM
// setzt sie bei jedem Lauf neu, damit eine geänderte Einstellung gilt und
// eine gelöschte nicht hängen bleibt; DNS-01 ist die einzige Stelle in LCM,
// die diesen Client nutzt.
func DNS01(provider challenge.Provider, checkNameservers []string) Solver {
	return func(m *resolver.SolverManager) error {
		useCheckNameservers(checkNameservers)
		return m.SetDNS01Provider(provider)
	}
}

func useCheckNameservers(nameservers []string) {
	dns01.SetDefaultClient(dns01.NewClient(&dns01.Options{RecursiveNameservers: nameservers}))
}

// HTTP01 beantwortet die Prüfung der CA über den Anbieter, der das Token
// ausliefert (ChallengeStore).
func HTTP01(provider challenge.Provider) Solver {
	return func(m *resolver.SolverManager) error { return m.SetHTTP01Provider(provider) }
}

// window ist das Zeitfenster, in dem die CA die Erneuerung empfiehlt (ARI).
type window struct{ Start, End time.Time }

// authority ist die Gegenstelle: die ACME-CA, mit der LCM ein Konto hat.
type authority interface {
	// Obtain stellt ein neues Zertifikat für die Namen aus.
	Obtain(ctx context.Context, names []string) (bundle, error)
	// Renew stellt das Nachfolge-Zertifikat aus und nennt der CA das alte
	// (ARI), mit neuem Schlüssel.
	Renew(ctx context.Context, names []string, old bundle) (bundle, error)
	// RenewalWindow fragt das empfohlene Erneuerungsfenster; nil, wenn die CA
	// kein ARI kann.
	RenewalWindow(ctx context.Context, leaf *x509.Certificate) (*window, error)
}

// legoCA ist die CA über einen lego-Client.
type legoCA struct{ client *lego.Client }

// connect verbindet sich mit der CA und richtet Konto und Challenge ein.
func connect(ctx context.Context, dataDir string, cfg Config) (authority, error) {
	if cfg.Solver == nil {
		return nil, errors.New("keine challenge gewählt")
	}
	client, err := openAccount(ctx, dataDir, cfg, func(acc *account) (*lego.Client, error) {
		conf := lego.NewConfig(acc)
		conf.CADirURL = cfg.DirectoryURL
		conf.UserAgent = "lcm"
		return lego.NewClient(conf)
	})
	if err != nil {
		return nil, err
	}
	if err := cfg.Solver(client.Challenge); err != nil {
		return nil, fmt.Errorf("challenge einrichten: %w", err)
	}
	return &legoCA{client: client}, nil
}

func (c *legoCA) Obtain(ctx context.Context, names []string) (bundle, error) {
	res, err := c.client.Certificate.Obtain(ctx, certificate.ObtainRequest{
		Domains: names,
		Bundle:  true,
		KeyType: certcrypto.EC256,
	})
	return toBundle(res, err)
}

func (c *legoCA) Renew(ctx context.Context, names []string, old bundle) (bundle, error) {
	// Ohne PrivateKey in der Ressource erzeugt lego einen neuen Schlüssel.
	res, err := c.client.Certificate.Renew(ctx,
		certificate.Resource{Domains: names, Certificate: old.certPEM, KeyType: certcrypto.EC256},
		&certificate.RenewOptions{Bundle: true, UseARICertID: true})
	return toBundle(res, err)
}

func toBundle(res *certificate.Resource, err error) (bundle, error) {
	if err != nil {
		return bundle{}, err
	}
	return bundle{certPEM: res.Certificate, keyPEM: res.PrivateKey}, nil
}

func (c *legoCA) RenewalWindow(ctx context.Context, leaf *x509.Certificate) (*window, error) {
	info, err := c.client.Certificate.GetRenewalInfo(ctx, leaf)
	if errors.Is(err, api.ErrNoARI) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &window{Start: info.SuggestedWindow.Start, End: info.SuggestedWindow.End}, nil
}
