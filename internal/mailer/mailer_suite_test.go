package mailer

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/datahearth/streamline/internal/testutil"
)

var serverCert tls.Certificate

func TestMailer(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Mailer Suite")
}

var _ = BeforeSuite(func() {
	DeferCleanup(testutil.InstallSlog())

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	Expect(err).NotTo(HaveOccurred())
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "fake smtp"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	Expect(err).NotTo(HaveOccurred())
	serverCert = tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}

	// Go loads the system roots once, on first TLS use, so this must precede it.
	caFile := filepath.Join(GinkgoT().TempDir(), "ca.pem")
	Expect(
		os.WriteFile(
			caFile,
			pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
			0o600,
		),
	).To(Succeed())
	GinkgoT().Setenv("SSL_CERT_FILE", caFile)
})
