//go:build linux && arm

package mkcert

import _ "embed"

//go:embed bin/mkcert-linux-arm
var binary []byte
