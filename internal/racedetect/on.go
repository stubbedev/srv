//go:build race

// Package racedetect reports whether the binary was built with -race.
// Allocation budgets use it to stand down: the race runtime instruments
// allocations and makes sync.Pool drop items at random, so counts taken
// under it do not describe the production binary.
package racedetect

// Enabled is true when built with -race.
const Enabled = true
