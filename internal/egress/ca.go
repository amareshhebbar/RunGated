package egress

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type CA struct {
	cert  *x509.Certificate
	key   *ecdsa.PrivateKey
	pem   []byte
	mu    sync.Mutex
	cache map[string]*tls.Certificate
}

func (c *CA) PEM() []byte { return c.pem }

func (c *CA) Pool() *x509.CertPool {
	p := x509.NewCertPool()
	p.AddCert(c.cert)
	return p
}

func LoadOrCreateCA(dir string) (*CA, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	certPath := filepath.Join(dir, "egress-ca.pem")
	keyPath := filepath.Join(dir, "egress-ca.key")
	cb, cerr := os.ReadFile(certPath)
	kb, kerr := os.ReadFile(keyPath)
	if cerr == nil && kerr == nil {
		cblock, _ := pem.Decode(cb)
		kblock, _ := pem.Decode(kb)
		if cblock == nil || kblock == nil {
			return nil, errors.New("egress CA files are corrupt")
		}
		cert, err := x509.ParseCertificate(cblock.Bytes)
		if err != nil {
			return nil, err
		}
		key, err := x509.ParseECPrivateKey(kblock.Bytes)
		if err != nil {
			return nil, err
		}
		return &CA{cert: cert, key: key, pem: cb, cache: map[string]*tls.Certificate{}}, nil
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 126))
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "Rungated Egress CA", Organization: []string{"Rungated"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(5, 0, 0),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		MaxPathLen:            0,
		MaxPathLenZero:        true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	cpem := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	kder, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kder}), 0o600); err != nil {
		return nil, err
	}
	if err := os.WriteFile(certPath, cpem, 0o644); err != nil {
		return nil, err
	}
	return &CA{cert: cert, key: key, pem: cpem, cache: map[string]*tls.Certificate{}}, nil
}

func (c *CA) Leaf(host string) (*tls.Certificate, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if lc, ok := c.cache[host]; ok && time.Now().Before(lc.Leaf.NotAfter.Add(-time.Hour)) {
		return lc, nil
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 126))
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: host},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	if ip := net.ParseIP(host); ip != nil {
		tmpl.IPAddresses = []net.IP{ip}
	} else {
		tmpl.DNSNames = []string{host}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, c.cert, &key.PublicKey, c.key)
	if err != nil {
		return nil, err
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	tc := &tls.Certificate{Certificate: [][]byte{der, c.cert.Raw}, PrivateKey: key, Leaf: leaf}
	if len(c.cache) > 2000 {
		c.cache = map[string]*tls.Certificate{}
	}
	c.cache[host] = tc
	return tc, nil
}
