package peer

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/stubbedev/srv/internal/constants"
	"github.com/stubbedev/srv/internal/fsutil"
)

const announceInterval = 30 * time.Second

const Expiry = 3 * announceInterval

type Entry struct {
	Name Name       `json:"name"`
	Addr netip.Addr `json:"addr"`
	Seen time.Time  `json:"seen"`
}

func (e Entry) live(now time.Time) bool {
	return now.Sub(e.Seen) < Expiry
}

type table map[Name]Entry

func (t table) observe(e Entry) (addrChanged bool) {
	prev, known := t[e.Name]
	t[e.Name] = e
	return !known || prev.Addr != e.Addr
}

func (t table) expire(now time.Time) (removed bool) {
	for name, e := range t {
		if !e.live(now) {
			delete(t, name)
			removed = true
		}
	}
	return removed
}

func (t table) sorted() []Entry {
	entries := make([]Entry, 0, len(t))
	for _, e := range t {
		entries = append(entries, e)
	}
	slices.SortFunc(entries, func(a, b Entry) int { return strings.Compare(string(a.Name), string(b.Name)) })
	return entries
}

func save(path string, entries []Entry) error {
	data, err := json.Marshal(entries)
	if err != nil {
		return fmt.Errorf("encode peer table: %w", err)
	}
	if err := fsutil.AtomicWriteFile(path, data, constants.FilePermDefault); err != nil {
		return fmt.Errorf("write peer table %s: %w", path, err)
	}
	return nil
}

func Load(path string, now time.Time) ([]Entry, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read peer table %s: %w", path, err)
	}
	var entries []Entry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("decode peer table %s: %w", path, err)
	}
	return slices.DeleteFunc(entries, func(e Entry) bool { return !e.live(now) }), nil
}

func Find(entries []Entry, name Name) (Entry, bool) {
	i := slices.IndexFunc(entries, func(e Entry) bool { return e.Name == name })
	if i < 0 {
		return Entry{}, false
	}
	return entries[i], true
}

func Resolve(path string, name Name, now time.Time) (netip.Addr, error) {
	entries, err := Load(path, now)
	if err != nil {
		return netip.Addr{}, err
	}
	e, ok := Find(entries, name)
	if !ok {
		return netip.Addr{}, fmt.Errorf("resolve peer %q: not announced on the LAN within the last %s (is a srv daemon with peer_name set, or `srv peer run`, running here?)", name, Expiry)
	}
	return e.Addr, nil
}
