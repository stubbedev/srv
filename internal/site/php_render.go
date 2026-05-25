// Package site — php_render.go exposes the install-php-extensions catalogue
// used by `srv scaffold php` to validate / auto-complete extension names and
// by the detection layer to filter out builtins.
//
// srv no longer generates a PHP Dockerfile of its own; the runtime container
// is now user-owned (write your own Dockerfile or run `srv scaffold php`).
// This file stays in the site package because the detection helpers in php.go
// reference the builtin-extension set when normalising composer.json output.
package site

import (
	"os"
	"sort"
)

// osStat is a tiny indirection so the file-existence helpers don't need to
// pull `os` into every caller's surface area.
var osStat = os.Stat

// ipeExtensions is the full list of PHP extensions supported by
// install-php-extensions (https://github.com/mlocati/docker-php-extension-installer).
var ipeExtensions = []string{
	"amqp", "apcu", "apcu_bc", "ast",
	"bcmath", "bitset", "blackfire", "brotli", "bz2",
	"calendar", "cassandra", "cmark", "csv",
	"dba", "ddtrace", "decimal", "ds",
	"ecma_intl", "enchant", "ev", "event", "excimer", "exif",
	"ffi", "ftp",
	"gd", "gearman", "geoip", "geos", "geospatial", "gettext", "gmagick", "gmp", "gnupg", "grpc",
	"http",
	"igbinary", "imagick", "imap", "inotify", "intl", "ion", "ioncube_loader", "ip2location",
	"jsmin", "json_post", "jsonpath", "judy",
	"ldap", "luasandbox", "lz4", "lzf",
	"mailparse", "maxminddb", "mbstring", "mcrypt", "md4c", "memcache", "memcached", "memprof",
	"mongodb", "msgpack", "mysqli",
	"oauth", "oci8", "odbc", "opcache", "opencensus", "openswoole", "opentelemetry", "operator",
	"parallel", "parle", "pcntl", "pcov",
	"pdo", "pdo_dblib", "pdo_firebird", "pdo_mysql", "pdo_oci", "pdo_odbc", "pdo_pgsql",
	"pdo_snowflake", "pdo_sqlite", "pdo_sqlsrv", "pgsql", "phalcon", "php_trie", "phpy",
	"pkcs11", "pq", "propro", "protobuf", "pspell", "psr",
	"raphf", "rdkafka", "redis", "relay",
	"saxon", "seasclick", "seaslog", "shmop", "simdjson", "simplexml", "smbclient", "snappy",
	"snmp", "snuffleupagus", "soap", "sockets", "sodium", "solr", "sourceguardian", "spx", "sqlsrv",
	"ssh2", "stomp", "swoole", "sync", "sysvmsg", "sysvsem", "sysvshm",
	"tensor", "tideways", "tidy", "timezonedb", "translit",
	"uopz", "uploadprogress", "uuid", "uv",
	"vips", "vld",
	"xattr", "xdebug", "xdiff", "xhprof", "xlswriter", "xmldiff", "xmlrpc", "xpass", "xsl",
	"yac", "yaml", "yar",
	"zephir_parser", "zip", "zmq", "zookeeper", "zstd",
}

// KnownPHPExtensions returns a sorted list of all PHP extensions supported by
// install-php-extensions. Callers must treat the result as read-only.
func KnownPHPExtensions() []string {
	exts := make([]string, len(ipeExtensions))
	copy(exts, ipeExtensions)
	sort.Strings(exts)
	return exts
}

// builtinExtensions are always compiled into PHP — no installation step needed.
var builtinExtensions = map[string]bool{
	"json":      true,
	"hash":      true,
	"openssl":   true,
	"sodium":    true,
	"filter":    true,
	"ctype":     true,
	"session":   true,
	"pcre":      true,
	"spl":       true,
	"standard":  true,
	"tokenizer": true,
}

// IsBuiltinPHPExtension reports whether ext ships pre-compiled into PHP.
func IsBuiltinPHPExtension(ext string) bool {
	return builtinExtensions[ext]
}

// NonBuiltinExtensions returns the input list filtered to extensions that
// install-php-extensions actually needs to install. Exposed (capitalised)
// so the scaffold command can reuse it.
func NonBuiltinExtensions(exts []string) []string {
	out := make([]string, 0, len(exts))
	for _, e := range exts {
		if !builtinExtensions[e] {
			out = append(out, e)
		}
	}
	return out
}

// =============================================================================
// Small filesystem helpers (used by site detection — previously lived in the
// deleted php_franken.go, kept here so all site-detection files share them).
// =============================================================================

func fileExists(path string) bool {
	info, err := osStat(path)
	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := osStat(path)
	return err == nil && info.IsDir()
}

func hasComposerPackagePrefix(composer *ComposerJSON, prefix string) bool {
	for key := range composer.Require {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}
