package util

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"github.com/er1c-zh/go-now/log"
	"math/big"
	m_rand "math/rand"
	"net"
	"runtime"
	"sort"
	"time"
)

type Cert struct {
	Cert    []byte
	Private []byte
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
		Type:  "CERTIFICATE",
		Bytes: certBytes,
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
		Cert:    certPEM.Bytes(),
		Private: certPrivKeyPEM.Bytes(),
	}, nil
}

func SignHost(hosts []string) (*tls.Certificate, error) {
	var err error
	template := &x509.Certificate{
		SerialNumber: big.NewInt(m_rand.Int63()),
		Subject: pkix.Name{
			Organization: []string{"Digger untrusted proxy"},
		},
		NotBefore:             time.Now().AddDate(0, 0, -2),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	for _, h := range hosts {
		if ip := net.ParseIP(h); ip != nil {
			template.IPAddresses = append(template.IPAddresses, ip)
		} else {
			template.DNSNames = append(template.DNSNames, h)
			template.Subject.CommonName = h
		}
	}
	hash := hashSorted(append(hosts, ":"+runtime.Version()))
	var csprng CounterEncryptorRand
	if csprng, err = NewCounterEncryptorRandFromKey(CA.PrivateKey, hash); err != nil {
		return nil, err
	}
	var certpriv crypto.Signer
	switch CA.PrivateKey.(type) {
	case *rsa.PrivateKey:
		if certpriv, err = rsa.GenerateKey(&csprng, 2048); err != nil {
			return nil, err
		}
	case *ecdsa.PrivateKey:
		if certpriv, err = ecdsa.GenerateKey(elliptic.P256(), &csprng); err != nil {
			return nil, err
		}
	default:
		err = fmt.Errorf("unsupported key type %T", CA.PrivateKey)
	}

	var derBytes []byte
	if derBytes, err = x509.CreateCertificate(&csprng, template, CA.Leaf, certpriv.Public(), CA.PrivateKey); err != nil {
		return nil, err
	}
	return &tls.Certificate{
		Certificate: [][]byte{derBytes, CA.Certificate[0]},
		PrivateKey:  certpriv,
	}, nil
}

func init() {
	defer log.Flush()
	var err error
	CA, err = tls.X509KeyPair(rootCACert, rootCAPK)
	if err != nil {
		log.Fatal("parse root CA cert fail: %s", err.Error())
		panic(err)
	}
	CA.Leaf, err = x509.ParseCertificate(CA.Certificate[0])
	if err != nil {
		log.Fatal("parse root CA fail: %s", err.Error())
		panic(err)
	}
	m_rand.Seed(time.Now().UnixNano())
}

func hashSorted(lst []string) []byte {
	c := make([]string, len(lst))
	copy(c, lst)
	sort.Strings(c)
	h := sha1.New()
	for _, s := range c {
		h.Write([]byte(s + ","))
	}
	return h.Sum(nil)
}

var (
	CA         tls.Certificate
	rootCACert = []byte(`-----BEGIN CERTIFICATE-----
MIIDYzCCAkugAwIBAgIUMr98Xy1rROm9Zuy+H/BYBPhcE3owDQYJKoZIhvcNAQEL
BQAwQTELMAkGA1UEBhMCQ04xGDAWBgNVBAoMD0RpZ2dlciBQcm94eSBDQTEYMBYG
A1UEAwwPRGlnZ2VyIFByb3h5IENBMB4XDTIwMDcyOTE4MDEzN1oXDTMwMDcyNzE4
MDEzN1owQTELMAkGA1UEBhMCQ04xGDAWBgNVBAoMD0RpZ2dlciBQcm94eSBDQTEY
MBYGA1UEAwwPRGlnZ2VyIFByb3h5IENBMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8A
MIIBCgKCAQEA0Y99717lg3dklFughe+P3Fk5BGZDUPimn9lWRbJFXRhH7MKvoC8M
zTAFkvyxhQjER3SC12RFY7brpORAf7xbaV1/i/pP1bnu/EyjuhARFnqOu9A3LwIg
iTsECFEI7l+eXlsGfnmKlJT4zFaoC+YKh0sFscwNPwYBIdftHTEAXXYtaRmc46/y
R/MZgLomfkSpEgwUiFh05x3UQxSG3JChykEB7ts+yKnT4R+QoBMWfRrgPs84WVIs
RLAgdyqvhlBpIpIryH+BZx27xd3Cd4wm6l3Uaro/SdkSwgauBJzOqEuMCbv8dMmJ
j4Or8BPEBPdq4sJXobBY+xJwBVtSAUJVkwIDAQABo1MwUTAdBgNVHQ4EFgQUbYAb
Q3AFho7GOYi1jW+dZ0fHXN0wHwYDVR0jBBgwFoAUbYAbQ3AFho7GOYi1jW+dZ0fH
XN0wDwYDVR0TAQH/BAUwAwEB/zANBgkqhkiG9w0BAQsFAAOCAQEAoklaVnTRzgvr
SosqNYCsVT1xp5oFwO0lpGfXFbjOOtDkJvwKXKw6C3GfnkS29q4bVbimvYefiL6c
ElGYS0Cpx3JCylwXK9EIPrJwlnesxZb8KnRy7GgNeSjuabkg3ArCESkCSKRHNvIp
Rkq/ZpMFfX4smjlTTonmxj4BYZJaesmvnqmcWJdrJtPdacgv+OTEgmwPxA+fimFo
sxSG7ImvvR+t4s9mazs2I9PjxB3lSEqp5DocVJ49aUpsGkhq+kJx9m2E0BubmEhk
QohvRrRLdbJk4kNT2QfS6Lyl+MIvEcMtHnHCDgpTP2+XTaeU3eGWRs3AjVtQjPav
sAKtQwaZLg==
-----END CERTIFICATE-----`)
	rootCAPK = []byte(`-----BEGIN RSA PRIVATE KEY-----
MIIEpQIBAAKCAQEA0Y99717lg3dklFughe+P3Fk5BGZDUPimn9lWRbJFXRhH7MKv
oC8MzTAFkvyxhQjER3SC12RFY7brpORAf7xbaV1/i/pP1bnu/EyjuhARFnqOu9A3
LwIgiTsECFEI7l+eXlsGfnmKlJT4zFaoC+YKh0sFscwNPwYBIdftHTEAXXYtaRmc
46/yR/MZgLomfkSpEgwUiFh05x3UQxSG3JChykEB7ts+yKnT4R+QoBMWfRrgPs84
WVIsRLAgdyqvhlBpIpIryH+BZx27xd3Cd4wm6l3Uaro/SdkSwgauBJzOqEuMCbv8
dMmJj4Or8BPEBPdq4sJXobBY+xJwBVtSAUJVkwIDAQABAoIBAQCPj+7laqxvKP7V
iAPrXZe/i7w84FXjhcSYo4qvypYsVbMIZsNsSG9LrkdTUBvJGJ1mmlH8fyvuSOUc
HGZ3W7F/+FalrYC92Vf4rgRINjOOo71euyDi6mEhwjVcAS/OJeFXoKJNSLSAX6Im
UoNjS2ARGXs4N6MndtSVu9gr9GLcoxjI9GzEAPQmOiV8nX6USRJsrYiHQDJb0k0M
g0jWNCQl/zTpo98aYKInRFJNv9YeyUAMx7WAtX9mYU8ChRQMThX8qY/hAI51gSIt
5l97S0CnJNPNDhl+4wZ+RXGoUvTgQOctIX2g0e1yR4flIJxFrgk0DgsGdulx/jzB
o2io8Qq5AoGBAOuWoaKecDbq7+9Et+rytOVeCUkRYWnTA8jRBLXvn1lDCprHNIjC
cal6IS0Onf6rwwnnsdL+bEYQrheVzBLt9ol1Q8TeL5p3TPGFZ9nqrCNlC/Fudh3M
VTUtotK2RoLoomv1YY5mOEn6EvF0Q0f16OlNTIBCut2grbvmgje66Kg1AoGBAOO3
j1zk/wCp5r4Qc6Mq8yx6TURC9BeGl8pTI2UDeJOyMRPuJIabBMs4bFpx1t7i0rn2
KRDw8FsgucQdrbHCL3jyUcTOUCfzMBDI2/cagc59j/+KmTky9Byc50Z/g0h2kZi+
Z3LlZW1tGWhvR1Vkbhgb2/24Izqhg/aLCSU+S4+nAoGAPf+bM++cOmejkwUznYoX
3xDbQrZnO3FD2rJfGf4gol4JSWhJRABf5yjz2Cazn5TWNCIcYxl/pwS2vBA473Ze
XhhVKFcMkgr5Xcos5WVjvcDW3seiH/9pISCMbAV6EvNj4yNldBMklxtPpulg12w4
ykUEb/CfurmRXxSvijkPB00CgYEAqlOWjClNA7YRvYCYvidWFKK2QKTD5wTpbJCb
HOdnvTG/u+SYtYYmI8tkYJJd4gFPFYGmXeGaJs9no+V/EkLpN1IpD0gydG9WOHfE
8COHjGgm2UFWMo6GQRCrfPPLwtvNM67Xuf0TzLGaG5+Af8LLBoVwG2ssDqLZDgQZ
Jx5dbmkCgYEA57S9hLfSx/cXPkCJNXx+UQiDQdHrSoPKmgm3DcL827OLf9yC+ebG
MYuDxrSAhxwlXUd0HFCt7Mlb/oFlzV9PR5bY6dOfXbPMLN1vDiwg8v4TONUDIEwQ
rSotqxIT4gdqARVTN8CNuyycgXwlilJWC2DKWvFiInnEQjPCdk0eIy4=
-----END RSA PRIVATE KEY-----`)
)
