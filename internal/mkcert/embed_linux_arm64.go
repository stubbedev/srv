//go:build linux && arm64

package mkcert

import _ "embed"

//go:embed bin/mkcert-linux-arm64
var binary []byte
