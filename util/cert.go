package util

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"github.com/er1c-zh/go-now/log"
	"math/big"
	"time"
)

type Cert struct {
	Cert *bytes.Buffer
	Private *bytes.Buffer
}
func GenerateCertificate() (*Cert, error) {
	pk, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		log.Error("GenerateKey fail: %s", err.Error())
		return nil, err
	}

	cert := &x509.Certificate{
		SerialNumber: big.NewInt(1220),
		Subject: pkix.Name{
			Organization:  []string{"Digger"},
			Country:       []string{"CN"},
			Province:      []string{""},
			Locality:      []string{""},
			StreetAddress: []string{""},
			PostalCode:    []string{""},
		},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().AddDate(10, 0, 0),
		SubjectKeyId: []byte{1, 2, 3, 4, 6},
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	certBytes, err := x509.CreateCertificate(rand.Reader, cert, cert, &pk.PublicKey, pk)

	certPEM := new(bytes.Buffer)
	err = pem.Encode(certPEM, &pem.Block{
		Type:    "CERTIFICATE",
		Bytes:   certBytes,
	})
	if err != nil {
		log.Error("pem.Encode fail: %s", err.Error())
	}
	certPrivKeyPEM := new(bytes.Buffer)
	err = pem.Encode(certPrivKeyPEM, &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(pk),
	})
	if err != nil {
		log.Error("pem.Encode fail: %s", err.Error())
	}
	return &Cert{
		Cert:    certPEM,
		Private: certPrivKeyPEM,
	}, nil
}

