package opcua

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gopcua/opcua/ua"
	"github.com/mwieczorkiewicz/opcua-mcp/internal/config"
)

// writeSelfSigned writes a PEM cert/key pair and returns their paths and the DER cert.
func writeSelfSigned(t *testing.T, dir string) (string, string, []byte) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPath := filepath.Join(dir, "client.pem")
	keyPath := filepath.Join(dir, "client.key")
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}), 0o600); err != nil {
		t.Fatal(err)
	}
	return certPath, keyPath, der
}

func TestAddChannelSecurityNoneIsNoop(t *testing.T) {
	c := NewClient(&config.OPCUAConfig{SecurityPolicy: "None", SecurityMode: "None", AuthMode: "anonymous"})
	opts, err := c.addChannelSecurity(context.Background(), nil)
	if err != nil || len(opts) != 0 {
		t.Fatalf("expected no options and no error, got %d options, err=%v", len(opts), err)
	}
}

func TestAddChannelSecurityRequiresKeyPair(t *testing.T) {
	c := NewClient(&config.OPCUAConfig{SecurityPolicy: "Basic256Sha256", SecurityMode: "SignAndEncrypt", AuthMode: "anonymous"})
	if _, err := c.addChannelSecurity(context.Background(), nil); err == nil {
		t.Fatal("expected error when cert/key files are missing")
	}
}

func TestLoadClientKeyPair(t *testing.T) {
	certPath, keyPath, der := writeSelfSigned(t, t.TempDir())
	cert, key, err := loadClientKeyPair(certPath, keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(cert) != string(der) || key == nil {
		t.Fatal("loaded key pair does not match written one")
	}
}

func TestVerifyServerCert(t *testing.T) {
	dir := t.TempDir()
	certPath, _, der := writeSelfSigned(t, dir)
	derPath := filepath.Join(dir, "server.der")
	if err := os.WriteFile(derPath, der, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{certPath, derPath} {
		if err := verifyServerCert(p, der); err != nil {
			t.Errorf("%s: expected match, got %v", filepath.Base(p), err)
		}
	}
	_, _, other := writeSelfSigned(t, t.TempDir())
	if err := verifyServerCert(certPath, other); err == nil {
		t.Error("expected mismatch error")
	}
}

func TestUserTokenType(t *testing.T) {
	cases := map[string]ua.UserTokenType{
		"anonymous":   ua.UserTokenTypeAnonymous,
		"username":    ua.UserTokenTypeUserName,
		"certificate": ua.UserTokenTypeCertificate,
	}
	for mode, want := range cases {
		if got := userTokenType(mode); got != want {
			t.Errorf("%s: got %v, want %v", mode, got, want)
		}
	}
}
