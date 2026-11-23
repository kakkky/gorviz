// Package tlscert は server の証明書を用意する。利用者が差し込んだものがなければ、初回に CA と server の証明書を作る
package tlscert

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"math/big"
	"net"
	"os"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/config"
	"github.com/kakkky/gorviz/internal/app/server/driver/filesystem"
)

const (
	caFile     = "server/tls/ca.pem"
	serverFile = "server/tls/server.pem"
	validFor   = 10 * 365 * 24 * time.Hour
)

// NewTLSConfig は平文 (Insecure) なら nil を返す。自分で作った CA を使うときは、相手が CA のハッシュで確かめられるよう、証明書の並びに CA を含める
func NewTLSConfig(cfg *config.Config, root *filesystem.Root) (*tls.Config, error) {
	if cfg.Insecure {
		return nil, nil
	}
	if cfg.TLSCertFile != "" || cfg.TLSKeyFile != "" {
		cert, err := tls.LoadX509KeyPair(cfg.TLSCertFile, cfg.TLSKeyFile)
		if err != nil {
			return nil, fmt.Errorf("load the tls certificate: %w", err)
		}
		return &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}, nil
	}
	cert, ca, err := load(root)
	if errors.Is(err, fs.ErrNotExist) {
		cert, ca, err = generate(root, cfg.TLSHosts)
	}
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(ca.Raw)
	slog.Info("serving with the self-generated CA; verify the server by this fingerprint", slog.String("ca_sha256", hex.EncodeToString(sum[:])), slog.String("ca_file", caFile))
	return &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}, nil
}

func load(root *filesystem.Root) (tls.Certificate, *x509.Certificate, error) {
	caPEM, err := root.ReadFile(caFile)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	serverPEM, err := root.ReadFile(serverFile)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	block, _ := pem.Decode(caPEM)
	if block == nil {
		return tls.Certificate{}, nil, fmt.Errorf("%s is broken", caFile)
	}
	ca, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	cert, err := tls.X509KeyPair(serverPEM, serverPEM)
	if err != nil {
		return tls.Certificate{}, nil, fmt.Errorf("%s is broken: %w", serverFile, err)
	}
	cert.Certificate = append(cert.Certificate, ca.Raw)
	return cert, ca, nil
}

func serial() (*big.Int, error) {
	return rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
}

func generate(root *filesystem.Root, hosts []string) (tls.Certificate, *x509.Certificate, error) {
	now := time.Now()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	sn, err := serial()
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	caTmpl := &x509.Certificate{
		SerialNumber: sn, Subject: pkix.Name{CommonName: "gorviz-server CA"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(validFor),
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign, BasicConstraintsValid: true, IsCA: true, MaxPathLenZero: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	ca, err := x509.ParseCertificate(caDER)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	if sn, err = serial(); err != nil {
		return tls.Certificate{}, nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: sn, Subject: pkix.Name{CommonName: "gorviz-server"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(validFor),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	if h, err := os.Hostname(); err == nil {
		hosts = append(hosts, h)
	}
	for _, h := range hosts {
		if ip := net.ParseIP(h); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		} else if h != "" {
			tmpl.DNSNames = append(tmpl.DNSNames, h)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, &key.PublicKey, caKey)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	serverPEM := append(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})...)
	// 片方だけ残って落ちたときに組み合わせが崩れないよう、作り直すときは両方を置き換える
	if err := root.Replace(serverFile, serverPEM); err != nil {
		return tls.Certificate{}, nil, err
	}
	if err := root.Replace(caFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER})); err != nil {
		return tls.Certificate{}, nil, err
	}
	return load(root)
}
