package advisories

import (
	"context"

	"LCM/internal/core/domain"
)

// Fake ist eine Quelle für Tests. QueryFunc/DetailsFunc liefern kanonische
// Antworten; die Zähler belegen, dass der Cache tatsächlich Abfragen spart.
type Fake struct {
	SourceName   string
	IsAvailable  bool
	IsLocal      bool
	QueryFunc    func(purls []string) (map[string][]Advisory, error)
	DetailsFunc  func(ids []string) (map[string]Detail, error)
	QueryCalls   int
	QueriedPurls []string
	DetailCalls  int
	DetailedIDs  []string
}

func (f *Fake) Name() string {
	if f.SourceName != "" {
		return f.SourceName
	}
	return domain.AdvisorySourceOSV
}

func (f *Fake) Available() bool { return f != nil && f.IsAvailable }

// IsLocal stellt eine lokale Quelle nach (siehe Source.Local) - damit lässt
// sich prüfen, dass der Zwischenspeicher dann übergangen wird.
func (f *Fake) Local() bool { return f != nil && f.IsLocal }

func (f *Fake) Query(_ context.Context, purls []string) (map[string][]Advisory, error) {
	f.QueryCalls++
	f.QueriedPurls = append(f.QueriedPurls, purls...)
	if f.QueryFunc != nil {
		return f.QueryFunc(purls)
	}
	return map[string][]Advisory{}, nil
}

func (f *Fake) Details(_ context.Context, ids []string) (map[string]Detail, error) {
	f.DetailCalls++
	f.DetailedIDs = append(f.DetailedIDs, ids...)
	if f.DetailsFunc != nil {
		return f.DetailsFunc(ids)
	}
	return map[string]Detail{}, nil
}

// FakeExploits ist eine Ausnutzungs-Quelle fuer Tests.
type FakeExploits struct {
	IsAvailable bool
	CVEs        map[string]bool
	Err         error
	Calls       int
}

func (f *FakeExploits) Available() bool { return f != nil && f.IsAvailable }

func (f *FakeExploits) ExploitedCVEs(context.Context) (map[string]bool, error) {
	f.Calls++
	if f.Err != nil {
		return nil, f.Err
	}
	return f.CVEs, nil
}
