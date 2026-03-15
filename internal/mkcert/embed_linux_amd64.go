//go:build linux && amd64

package mkcert

import _ "embed"

//go:embed bin/mkcert-linux-amd64
var binary []byte
