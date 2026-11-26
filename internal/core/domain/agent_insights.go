package domain

import "LCM/internal/core/agentcaps"

// Befund zum lcm-agent für die Ampel (siehe TrafficLight): Er geht in die EINE
// Status-Pille ein, mit Text als Grund - keine zweite Anzeige daneben.
//
//	unsupported  rot    Agent unter der Mindestversion: LCM bietet nur noch
//	                    „Agent aktualisieren“ an
//	outdated     gelb   Agent kennt Funktionen nicht, die LCM anbietet
//	newer        gelb   Agent neuer als LCM: LCM bietet nur an, was es kennt
//	rolledBack   gelb   Das letzte Agent-Update ist gescheitert, der Agent ist
//	                    auf sein voriges Binary zurückgerollt
//	linkUnstable gelb   Die Verbindung riss in der letzten Stunde mehr als
//	                    AgentUnstableDrops Mal ungewollt ab
//
// Die deutschen Sätze sind für alles ohne Sprachkatalog (API, Benachrichtigungen);
// die Oberfläche setzt den Text aus den Parametern in ihrer Sprache zusammen.

// agentInsight bewertet den Agent. critical ist wahr, wenn er den Server rot färbt.
func agentInsight(r *agentcaps.Report) (note *StatusInsight, critical bool) {
	if r == nil {
		return nil, false
	}
	params := map[string]string{
		"version": r.AgentVersion, "server": r.ServerVersion, "min": r.MinVersion, "needs": r.NeedsVersion,
		"failed": r.RolledBack,
	}
	var in StatusInsight
	// Der Rollback ist der genauere Befund: Er sagt, WARUM der Agent veraltet
	// ist. Unter der Mindestversion bleibt „zu alt“ die stärkere Aussage (rot).
	if r.RolledBack != "" && r.State != agentcaps.StateUnsupported {
		in = insight("warning", "agentRolledBack",
			"Agent-Update gescheitert: lcm-agent "+r.RolledBack+" ließ sich nicht verbinden, der Agent ist auf "+
				r.AgentVersion+" zurückgerollt. Prüfe das Journal des Servers, bevor du es erneut versuchst.", params)
		return &in, false
	}
	switch r.State {
	case agentcaps.StateUnsupported:
		in = insight("critical", "agentUnsupported",
			"Agent zu alt: lcm-agent "+r.AgentVersion+" liegt unter der Mindestversion "+r.MinVersion+
				" - LCM bietet für diesen Server nur noch „Agent aktualisieren“ an.", params)
		return &in, true
	case agentcaps.StateOutdated:
		in = insight("warning", "agentOutdated",
			"Agent veraltet: lcm-agent "+r.AgentVersion+" kennt nicht alles, was dieses LCM anbietet "+
				"(braucht Agent ≥ "+r.NeedsVersion+"). Aktualisiere den Agent.", params)
	case agentcaps.StateNewer:
		in = insight("warning", "agentNewer",
			"Agent neuer als LCM: lcm-agent "+r.AgentVersion+" ist neuer als dieses LCM ("+r.ServerVersion+
				"). LCM bietet nur Funktionen an, die es selbst kennt - aktualisiere LCM.", params)
	default:
		return nil, false
	}
	return &in, false
}

// agentLinkInsight meldet eine instabile Verbindung des Agents (gelb) - als
// Befund der Ampel, nicht als zweite Anzeige.
func agentLinkInsight(l *AgentLink) *StatusInsight {
	if !l.Unstable() {
		return nil
	}
	in := insight("warning", "agentLinkUnstable",
		"Verbindung instabil: Der Agent ist in der letzten Stunde "+itoa(l.UnintendedLastHour)+
			" Mal ungewollt getrennt worden, zuletzt: "+dropLabel(l.LastCause)+
			". Prüfe Netz, Proxy und Journal des Servers (lcm-agent status).",
		map[string]string{"count": itoa(l.UnintendedLastHour), "disconnect": l.LastCause})
	return &in
}
