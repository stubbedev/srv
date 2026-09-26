// Package mkcert is the mkcert local-CA implementation, vendored into srv
// from github.com/FiloSottile/mkcert (revision 1c1dc4e, v1.4.4+). It creates
// and trusts a local development CA and issues certificates for local
// hostnames, so srv no longer shells out to a system mkcert binary.
//
// Vendoring is BSD licensed by The mkcert Authors; see LICENSE and AUTHORS in
// this directory. Local modifications relative to upstream:
//
//   - the CLI (main.go), flag parsing and console output are gone; every
//     operation returns errors and structured results instead
//   - CSR signing, PKCS#12 export, ECDSA keys, client certificates and the
//     Java trust store are dropped (srv never used them)
//   - privileged (sudo) commands honor srv's non-interactive mode so surfaces
//     without a TTY (the MCP server) fail fast instead of hanging on a prompt
//   - trust-store and certutil detection is lazy, so a plain `srv start` does
//     no package-init subprocess probing
//
// The CA on disk stays byte-for-byte compatible with the standalone mkcert
// tool: same CAROOT location, file names, key sizes and certificate profile.
// A CA created by `mkcert -install` is recognized (and vice versa).
package mkcert
