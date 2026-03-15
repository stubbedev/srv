//go:build darwin && arm64

package mkcert

import _ "embed"

//go:embed bin/mkcert-darwin-arm64
var binary []byte
