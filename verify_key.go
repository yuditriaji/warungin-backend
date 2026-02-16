package main

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
)

func main() {
	pubKeyPEM := `-----BEGIN PUBLIC KEY-----
MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAxesnFr7jjGH4DYj+elVP
Nd0oJVpHEMga6U7yhRW+hsEootxVST25AHV82KHqYTRlL7BwWABzJrHYP3GeKBLP
KXgPGbCz7Aqxo22fEDWnMLFBdHg8yXokM+3YlBuz5ksSUp8VIVmNeDk1XOlHR4VO
Qo+a+JB/F2GGJJ2nHGeAdiG/F+CEAPKV0OmnLLWbHT8Q9Ek+F2EAjrW5aG+/+1t+
Ma253Rbhv5CP43C+maQHPOqZx7S5+94XNsttga6nFA6YohNjZtshNM4O58kv9aJQ
8euXJN0raJsz2g9uZ/Pk9/1nwQxdlfUTFaJktPtrArjYbIl1+fXGpDeSQcfNgrWq
YQIDAQAB
-----END PUBLIC KEY-----`

	block, _ := pem.Decode([]byte(pubKeyPEM))
	if block == nil {
		fmt.Println("Failed to decode PEM")
		return
	}

	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		fmt.Printf("Failed to parse public key: %v\n", err)
		return
	}

	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		fmt.Println("Not an RSA public key")
		return
	}

	modulus := rsaPub.N.Bytes()
	fmt.Printf("Modulus Prefix: %X\n", modulus[:10])
}
