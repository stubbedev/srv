package peer

import (
	"encoding/json"
	"errors"
	"fmt"
)

const protocolVersion = 1

const maxMessageSize = 512

type message struct {
	Version int  `json:"v"`
	Name    Name `json:"name"`
	Reply   bool `json:"reply,omitempty"`
}

func encode(m message) ([]byte, error) {
	m.Version = protocolVersion
	data, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("encode peer announce for %q: %w", m.Name, err)
	}
	return data, nil
}

func decode(data []byte) (message, error) {
	var m message
	if err := json.Unmarshal(data, &m); err != nil {
		return message{}, fmt.Errorf("decode peer announce: %w", err)
	}
	if m.Version != protocolVersion {
		return message{}, fmt.Errorf("decode peer announce: protocol version %d, want %d", m.Version, protocolVersion)
	}
	if m.Name == "" {
		return message{}, errors.New("decode peer announce: no name")
	}
	name, err := ParseName(string(m.Name))
	if err != nil {
		return message{}, fmt.Errorf("decode peer announce: %w", err)
	}
	m.Name = name
	return m, nil
}
