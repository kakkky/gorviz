package serverapi

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"strings"
)

// NewHTTPClient は server の証明書を、caFile の CA か、server が自分で作った CA のハッシュ (caFingerprint, SHA-256 の 16 進) で確かめる。
// どちらも空ならシステムの CA で確かめる
func NewHTTPClient(caFile, caFingerprint string) (*http.Client, error) {
	cfg, err := TLSConfig(caFile, caFingerprint)
	if err != nil {
		return nil, err
	}
	return &http.Client{Transport: &http.Transport{TLSClientConfig: cfg, Proxy: http.ProxyFromEnvironment}}, nil
}

func TLSConfig(caFile, caFingerprint string) (*tls.Config, error) {
	switch {
	case caFile != "":
		pem, err := os.ReadFile(caFile)
		if err != nil {
			return nil, err
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, errors.New("no certificate in the CA file")
		}
		return &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}, nil
	case caFingerprint != "":
		want := strings.ToLower(strings.ReplaceAll(caFingerprint, ":", ""))
		// 名前は自分で作った CA の証明書に入らないことがあるので、ハッシュの合う CA でつながりだけを確かめる
		return &tls.Config{
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: true,
			VerifyConnection: func(cs tls.ConnectionState) error {
				pool := x509.NewCertPool()
				found := false
				for _, c := range cs.PeerCertificates {
					sum := sha256.Sum256(c.Raw)
					if hex.EncodeToString(sum[:]) == want {
						pool.AddCert(c)
						found = true
					}
				}
				if !found || len(cs.PeerCertificates) == 0 {
					return errors.New("the server certificate is not signed by the CA with the given fingerprint")
				}
				inter := x509.NewCertPool()
				for _, c := range cs.PeerCertificates[1:] {
					inter.AddCert(c)
				}
				_, err := cs.PeerCertificates[0].Verify(x509.VerifyOptions{Roots: pool, Intermediates: inter})
				return err
			},
		}, nil
	}
	return &tls.Config{MinVersion: tls.VersionTLS12}, nil
}
