// Copyright 2018 The mkcert Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package mkcert

import (
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"golang.org/x/net/idna"
)

var hostnameRegexp = regexp.MustCompile(`(?i)^(\*\.)?[0-9a-z_-]([0-9a-z._-]*[0-9a-z_-])?$`)

// validateHostnames punycodes and validates every name the way the standalone
// mkcert CLI does before signing, accepting hostnames, IPs, emails and URLs.
func validateHostnames(hosts []string) ([]string, error) {
	validated := make([]string, len(hosts))
	for i, name := range hosts {
		if ip := net.ParseIP(name); ip != nil {
			validated[i] = name
			continue
		}
		if email, err := mail.ParseAddress(name); err == nil && email.Address == name {
			validated[i] = name
			continue
		}
		if uriName, err := url.Parse(name); err == nil && uriName.Scheme != "" && uriName.Host != "" {
			validated[i] = name
			continue
		}
		punycode, err := idna.ToASCII(name)
		if err != nil {
			return nil, fmt.Errorf("%q is not a valid hostname, IP, URL or email: %w", name, err)
		}
		if !hostnameRegexp.MatchString(punycode) {
			return nil, fmt.Errorf("%q is not a valid hostname, IP, URL or email", name)
		}
		validated[i] = punycode
	}
	return validated, nil
}

// makeCert issues a leaf certificate for hosts, writing the PEM certificate
// and PKCS#8 key to the given paths. Validity is 2 years and 3 months, which
// is always less than 825 days, the limit that macOS/iOS apply to all
// certificates, including custom roots.
// See https://support.apple.com/en-us/HT210176.
func (ca *localCA) makeCert(hosts []string, certFile, keyFile string) error {
	if ca.caKey == nil {
		return errors.New("can't create new certificates because the CA key (rootCA-key.pem) is missing")
	}

	priv, err := generateKey(false)
	if err != nil {
		return fmt.Errorf("failed to generate certificate key: %w", err)
	}
	pub := priv.Public()

	serial, err := randomSerialNumber()
	if err != nil {
		return err
	}
	tpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization:       []string{"mkcert development certificate"},
			OrganizationalUnit: []string{userAndHostname},
		},

		NotBefore: time.Now(), NotAfter: time.Now().AddDate(2, 3, 0),

		KeyUsage: x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
	}

	for _, h := range hosts {
		if ip := net.ParseIP(h); ip != nil {
			tpl.IPAddresses = append(tpl.IPAddresses, ip)
		} else if email, err := mail.ParseAddress(h); err == nil && email.Address == h {
			tpl.EmailAddresses = append(tpl.EmailAddresses, h)
		} else if uriName, err := url.Parse(h); err == nil && uriName.Scheme != "" && uriName.Host != "" {
			tpl.URIs = append(tpl.URIs, uriName)
		} else {
			tpl.DNSNames = append(tpl.DNSNames, h)
		}
	}

	if len(tpl.IPAddresses) > 0 || len(tpl.DNSNames) > 0 || len(tpl.URIs) > 0 {
		tpl.ExtKeyUsage = append(tpl.ExtKeyUsage, x509.ExtKeyUsageServerAuth)
	}

	cert, err := x509.CreateCertificate(rand.Reader, tpl, ca.caCert, pub, ca.caKey)
	if err != nil {
		return fmt.Errorf("failed to generate certificate: %w", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert})
	privDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return fmt.Errorf("failed to encode certificate key: %w", err)
	}
	privPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER})

	if certFile == keyFile {
		if err := os.WriteFile(keyFile, append(certPEM, privPEM...), 0o600); err != nil {
			return fmt.Errorf("failed to save certificate and key: %w", err)
		}
		return nil
	}
	// Both halves are staged as temp siblings and swapped in via renames.
	// Two plain WriteFiles could interleave with another issuer and pair one
	// issuer's cert with the other's key — a state later checks consider
	// valid (both files exist, the cert parses) and Traefik rejects forever.
	certTmp, err := writeTempSibling(certFile, certPEM, 0o644)
	if err != nil {
		return fmt.Errorf("failed to save certificate: %w", err)
	}
	keyTmp, err := writeTempSibling(keyFile, privPEM, 0o600)
	if err != nil {
		_ = os.Remove(certTmp)
		return fmt.Errorf("failed to save certificate key: %w", err)
	}
	if err := os.Rename(certTmp, certFile); err != nil {
		_ = os.Remove(certTmp)
		_ = os.Remove(keyTmp)
		return fmt.Errorf("failed to save certificate: %w", err)
	}
	if err := os.Rename(keyTmp, keyFile); err != nil {
		_ = os.Remove(keyTmp)
		return fmt.Errorf("failed to save certificate key: %w", err)
	}
	return nil
}

// writeTempSibling stages data next to path for an atomic rename swap.
func writeTempSibling(path string, data []byte, perm os.FileMode) (string, error) {
	f, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return "", err
	}
	name := f.Name()
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(name)
		return "", err
	}
	if err := f.Chmod(perm); err != nil {
		_ = f.Close()
		_ = os.Remove(name)
		return "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(name)
		return "", err
	}
	return name, nil
}
