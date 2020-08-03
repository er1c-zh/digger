package util

import "testing"

func TestGenerateCertificate(t *testing.T) {
	cert, err := GenerateCertificate()
	if err != nil {
		t.Error(err)
		return
	}
	t.Logf("cert: %s", string(cert.Cert))
	t.Logf("private: %s", string(cert.Private))
}
