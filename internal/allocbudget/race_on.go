//go:build race

package allocbudget

// raceEnabled is true when built with -race: the race runtime instruments
// allocations and makes sync.Pool drop items at random, so counts taken
// under it do not describe the production binary.
const raceEnabled = true
