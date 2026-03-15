//go:build darwin && amd64

package mkcert

import _ "embed"

//go:embed bin/mkcert-darwin-amd64
var binary []byte
