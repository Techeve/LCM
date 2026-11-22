package main

import (
	"fmt"
	"io"
	"strings"

	"LCM/internal/control"
	"LCM/internal/core/services"
	"LCM/internal/netfilter"
)

const networkUsage = "verwendung: lcm [-data <verzeichnis>] network status | reset <ui|agent|mcp>"

// runNetworkCommand führt `lcm network status` bzw. `lcm network reset
// <endpunkt>` gegen den laufenden Dienst aus und liefert den Exit-Code.
func runNetworkCommand(dataDir string, args []string, out, errOut io.Writer) int {
	endpoint, ok := parseNetworkArgs(args)
	if !ok {
		fmt.Fprintln(errOut, networkUsage)
		return 2
	}
	socket, err := controlSocketPath(dataDir, packagedDataDir)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return 1
	}
	if args[0] == "status" {
		return networkStatus(socket, out, errOut)
	}
	return networkReset(socket, endpoint, out, errOut)
}

// parseNetworkArgs prüft die Aufrufform; der Endpunkt ist nur bei reset belegt.
func parseNetworkArgs(args []string) (endpoint netfilter.Endpoint, ok bool) {
	switch {
	case len(args) == 1 && args[0] == "status":
		return "", true
	case len(args) == 2 && args[0] == "reset":
		return netfilter.ParseEndpoint(args[1])
	}
	return "", false
}

func networkStatus(socket string, out, errOut io.Writer) int {
	var views []services.NetworkView
	if err := control.Call(socket, cmdNetworkStatus, &views); err != nil {
		fmt.Fprintln(errOut, callErrorText(socket, err))
		return 1
	}
	printNetworkStatus(out, views)
	return 0
}

func networkReset(socket string, endpoint netfilter.Endpoint, out, errOut io.Writer) int {
	if err := control.CallWithArg(socket, cmdNetworkReset, string(endpoint), nil); err != nil {
		fmt.Fprintln(errOut, callErrorText(socket, err))
		return 1
	}
	fmt.Fprintf(out, "Netzbereiche für %s zurückgesetzt: Ab jetzt sind alle Netze erlaubt.\n", endpoint.Label())
	return 0
}

func printNetworkStatus(w io.Writer, views []services.NetworkView) {
	fmt.Fprintln(w, "Erlaubte Netzbereiche je Dienst:")
	for _, v := range views {
		endpoint := netfilter.Endpoint(v.Endpoint)
		fmt.Fprintf(w, "  %-14s %s\n", endpoint.Label(), networkListText(v))
	}
}

func networkListText(v services.NetworkView) string {
	if len(v.Allowed) == 0 {
		return "alle Netze erlaubt"
	}
	return strings.Join(v.Allowed, ", ")
}
