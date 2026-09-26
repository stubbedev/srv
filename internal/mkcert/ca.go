// Copyright 2018 The mkcert Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mkcert

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	// The Subject Key Identifier is defined as the SHA-1 hash of the public
	// key by RFC 5280; the primitive choice is mandated by the spec, not a
	// security decision of this package.
	"crypto/sha1" //nolint:gosec // required by RFC 5280 for SKID
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"os/user"
	"path/filepath"
	"time"
)

const (
	rootName    = "rootCA.pem"
	rootKeyName = "rootCA-key.pem"
)

// userAndHostname identifies the CA owner in certificate subjects, matching
// the standalone mkcert's "user@hostname" OU.
var userAndHostname = buildUserAndHostname()

func buildUserAndHostname() string {
	u, err := user.Current()
	if err != nil {
		return ""
	}
	name := u.Username + "@"
	if h, err := os.Hostname(); err == nil {
		name += h
	}
	if u.Name != "" && u.Name != u.Username {
		name += " (" + u.Name + ")"
	}
	return name
}

// localCA is a loaded (and, when missing, freshly created) local CA.
type localCA struct {
	caroot string

	caCert *x509.Certificate
	caKey  crypto.PrivateKey

	created bool // a fresh CA was generated during this load
}

func (ca *localCA) path(name string) string {
	return filepath.Join(ca.caroot, name)
}

// loadCA loads the local CA from CAROOT, creating it when absent. A CA
// without a key is valid in install-only (keyless) mode, mirroring upstream.
func loadCA() (*localCA, error) {
	ca := &localCA{caroot: CAROOT()}
	if ca.caroot == "" {
		return nil, errors.New("failed to find the default CA location, set one as the CAROOT env var")
	}
	if err := os.MkdirAll(ca.caroot, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create the CA directory: %w", err)
	}

	if !pathExists(ca.path(rootName)) {
		if err := ca.newCA(); err != nil {
			return nil, err
		}
		ca.created = true
	}

	certPEM, err := os.ReadFile(ca.path(rootName))
	if err != nil {
		return nil, fmt.Errorf("failed to read the CA certificate: %w", err)
	}
	certDER, _ := pem.Decode(certPEM)
	if certDER == nil || certDER.Type != "CERTIFICATE" {
		return nil, errors.New("failed to read the CA certificate: unexpected content")
	}
	if ca.caCert, err = x509.ParseCertificate(certDER.Bytes); err != nil {
		return nil, fmt.Errorf("failed to parse the CA certificate: %w", err)
	}

	if !pathExists(ca.path(rootKeyName)) {
		return ca, nil // keyless mode, where only -install works
	}

	keyPEM, err := os.ReadFile(ca.path(rootKeyName))
	if err != nil {
		return nil, fmt.Errorf("failed to read the CA key: %w", err)
	}
	keyDER, _ := pem.Decode(keyPEM)
	if keyDER == nil || keyDER.Type != "PRIVATE KEY" {
		return nil, errors.New("failed to read the CA key: unexpected content")
	}
	if ca.caKey, err = x509.ParsePKCS8PrivateKey(keyDER.Bytes); err != nil {
		return nil, fmt.Errorf("failed to parse the CA key: %w", err)
	}
	return ca, nil
}

// newCA generates and stores a fresh local CA. The key is RSA-3072 and the
// certificate lives 10 years, matching upstream mkcert byte for byte.
func (ca *localCA) newCA() error {
	priv, err := generateKey(true)
	if err != nil {
		return fmt.Errorf("failed to generate the CA key: %w", err)
	}
	pub := priv.Public()

	spkiASN1, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return fmt.Errorf("failed to encode public key: %w", err)
	}

	var spki struct {
		Algorithm        pkix.AlgorithmIdentifier
		SubjectPublicKey asn1.BitString
	}
	if _, err := asn1.Unmarshal(spkiASN1, &spki); err != nil {
		return fmt.Errorf("failed to decode public key: %w", err)
	}

	skid := sha1.Sum(spki.SubjectPublicKey.Bytes) //nolint:gosec // SKID is SHA-1 by RFC 5280 definition

	serial, err := randomSerialNumber()
	if err != nil {
		return err
	}
	tpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization:       []string{"mkcert development CA"},
			OrganizationalUnit: []string{userAndHostname},

			// The CommonName is required by iOS to show the certificate in the
			// "Certificate Trust Settings" menu.
			// https://github.com/FiloSottile/mkcert/issues/47
			CommonName: "mkcert " + userAndHostname,
		},
		SubjectKeyId: skid[:],

		NotAfter:  time.Now().AddDate(10, 0, 0),
		NotBefore: time.Now(),

		KeyUsage: x509.KeyUsageCertSign,

		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLenZero:        true,
	}

	cert, err := x509.CreateCertificate(rand.Reader, tpl, tpl, pub, priv)
	if err != nil {
		return fmt.Errorf("failed to generate CA certificate: %w", err)
	}

	privDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return fmt.Errorf("failed to encode CA key: %w", err)
	}
	if err := os.WriteFile(ca.path(rootKeyName), pem.EncodeToMemory(
		&pem.Block{Type: "PRIVATE KEY", Bytes: privDER}), 0o400); err != nil {
		return fmt.Errorf("failed to save CA key: %w", err)
	}

	if err := os.WriteFile(ca.path(rootName), pem.EncodeToMemory(
		&pem.Block{Type: "CERTIFICATE", Bytes: cert}), 0o644); err != nil {
		return fmt.Errorf("failed to save CA certificate: %w", err)
	}
	return nil
}

// caUniqueName is the name the CA is enrolled under in NSS and Windows trust
// stores; the serial keeps distinct local CAs from clobbering each other.
func (ca *localCA) caUniqueName() string {
	return "mkcert development CA " + ca.caCert.SerialNumber.String()
}

// generateKey returns the RSA key used for the CA (3072) and leaves (2048),
// exactly as upstream mkcert.
func generateKey(rootCA bool) (*rsa.PrivateKey, error) {
	if rootCA {
		return rsa.GenerateKey(rand.Reader, 3072)
	}
	return rsa.GenerateKey(rand.Reader, 2048)
}

func randomSerialNumber() (*big.Int, error) {
	serialNumberLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	return rand.Int(rand.Reader, serialNumberLimit)
}
