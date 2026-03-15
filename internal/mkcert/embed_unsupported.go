//go:build !((linux && (amd64 || arm64 || arm)) || (darwin && (amd64 || arm64)))

package mkcert

// binary is nil on unsupported platforms; Run will return ErrUnsupported.
var binary []byte
