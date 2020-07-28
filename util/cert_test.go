package util

import "testing"

func TestGenerateCertificate(t *testing.T) {
	cert, err := GenerateCertificate()
	if err != nil {
		t.Error(err)
		return
	}
	t.Logf("cert: %s", cert.Cert.String())
	t.Logf("private: %s", cert.Private.String())
}
