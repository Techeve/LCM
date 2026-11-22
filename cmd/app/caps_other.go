//go:build !linux

package main

// dropAmbientCaps: Ambient-Capabilities gibt es nur unter Linux.
func dropAmbientCaps() {}
