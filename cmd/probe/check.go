package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"LCM/internal/probe/host"
)

// scopeText erklärt die Herkunft in Worten.
var scopeText = map[string]string{
	host.ScopeSystem:    "eigener Kernel (VM oder Blech)",
	host.ScopeContainer: "Wert des Containers",
	host.ScopeHost:      "Wert des Hosts, durchgereicht",
	host.ScopeMixed:     "gemischt: Benutzer und System vom Container, iowait und steal vom Host",
}

func checkCommand(args []string) error {
	fs := flag.NewFlagSet("check", flag.ExitOnError)
	root := fs.String("root", "", "Wurzel statt / (für eingefrorene Kopien)")
	_ = fs.Parse(args)

	src := host.Detect(*root)
	kind := "VM oder Blech"
	if src.Container {
		kind = "Container"
		if src.LXCFS {
			kind += " mit lxcfs"
		}
	}
	fmt.Printf("System: %s\n\n", kind)
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "Wert\tQuelle\tHerkunft")
	for _, key := range host.SourceKeys {
		origin, ok := src.Values[key]
		if !ok {
			fmt.Fprintf(tw, "%s\t-\tnicht lesbar\n", key)
			continue
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", key, origin.Path, scopeText[origin.Scope])
	}
	tw.Flush()
	fmt.Printf("\nPlatten: %s\n", strings.Join(src.Disks, ", "))
	return nil
}
