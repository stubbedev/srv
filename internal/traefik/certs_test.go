package traefik

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// generateTestCert creates a test certificate file with the given expiry.
func generateTestCert(t *testing.T, path string, notAfter time.Time) {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate private key: %v", err)
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Test"},
		},
		NotBefore:             time.Now(),
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("failed to create certificate: %v", err)
	}

	certOut, err := os.Create(path)
	if err != nil {
		t.Fatalf("failed to create cert file: %v", err)
	}
	defer certOut.Close()

	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}); err != nil {
		t.Fatalf("failed to write cert: %v", err)
	}
}

func TestParseCertFile(t *testing.T) {
	t.Run("non-existent file", func(t *testing.T) {
		info := parseCertFile("/non/existent/path.crt")
		if info.Exists {
			t.Error("expected Exists=false for non-existent file")
		}
	})

	t.Run("invalid PEM file", func(t *testing.T) {
		tmpDir := t.TempDir()
		certPath := filepath.Join(tmpDir, "invalid.crt")
		if err := os.WriteFile(certPath, []byte("not a valid pem"), 0o644); err != nil {
			t.Fatalf("failed to write file: %v", err)
		}

		info := parseCertFile(certPath)
		// Invalid/corrupt certs should be treated as non-existent so they get regenerated
		if info.Exists {
			t.Error("expected Exists=false for invalid/corrupt cert file")
		}
	})

	t.Run("valid certificate not expired", func(t *testing.T) {
		tmpDir := t.TempDir()
		certPath := filepath.Join(tmpDir, "valid.crt")
		expiry := time.Now().Add(365 * 24 * time.Hour) // 1 year from now
		generateTestCert(t, certPath, expiry)

		info := parseCertFile(certPath)
		if !info.Exists {
			t.Error("expected Exists=true")
		}
		if info.IsExpired {
			t.Error("expected IsExpired=false for valid cert")
		}
		if info.DaysLeft < 364 || info.DaysLeft > 366 {
			t.Errorf("expected DaysLeft around 365, got %d", info.DaysLeft)
		}
	})

	t.Run("expired certificate", func(t *testing.T) {
		tmpDir := t.TempDir()
		certPath := filepath.Join(tmpDir, "expired.crt")
		expiry := time.Now().Add(-24 * time.Hour) // Expired yesterday
		generateTestCert(t, certPath, expiry)

		info := parseCertFile(certPath)
		if !info.Exists {
			t.Error("expected Exists=true")
		}
		if !info.IsExpired {
			t.Error("expected IsExpired=true for expired cert")
		}
		if info.DaysLeft >= 0 {
			t.Errorf("expected negative DaysLeft for expired cert, got %d", info.DaysLeft)
		}
	})

	t.Run("certificate expiring soon", func(t *testing.T) {
		tmpDir := t.TempDir()
		certPath := filepath.Join(tmpDir, "expiring.crt")
		expiry := time.Now().Add(7 * 24 * time.Hour) // 7 days from now
		generateTestCert(t, certPath, expiry)

		info := parseCertFile(certPath)
		if !info.Exists {
			t.Error("expected Exists=true")
		}
		if info.IsExpired {
			t.Error("expected IsExpired=false for cert expiring soon")
		}
		if info.DaysLeft < 6 || info.DaysLeft > 8 {
			t.Errorf("expected DaysLeft around 7, got %d", info.DaysLeft)
		}
	})
}

func TestCertInfo(t *testing.T) {
	t.Run("zero value", func(t *testing.T) {
		info := CertInfo{}
		if info.Exists {
			t.Error("expected Exists=false for zero value")
		}
		if info.IsExpired {
			t.Error("expected IsExpired=false for zero value")
		}
	})
}
