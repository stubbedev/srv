package mkcert

import _ "embed"

// binary holds the mkcert binary built from third_party/mkcert.
// It is populated at build time by the Makefile / Nix flake before go build runs.
//
//go:embed bin/mkcert
var binary []byte
