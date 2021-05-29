package swarm

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"io"
	"log"
	"math/big"
	"net"

	"github.com/pion/stun"
)

func addrFromStun(conn *net.UDPConn) (string, error) {
	raddr, err := net.ResolveUDPAddr("udp4", "stun.l.google.com:19302")
	if err != nil {
		return "", err
	}
	// building binding request with random transaction id.
	message := stun.MustBuild(stun.TransactionID, stun.BindingRequest)
	// must use manual WriteToUDP instead of client to preserve port multiplex (client must use DialUDP)
	_, err = conn.WriteToUDP(message.Raw, raddr)
	if err != nil {
		return "", err
	}
	// read in the first response and assume it is correct
	// ideally you would check this and retry until you got a good response
	buf := make([]byte, 1024)
	n, _, err := conn.ReadFromUDP(buf)
	if err != nil {
		return "", err
	}
	buf = buf[:n]
	if !stun.IsMessage(buf) {
		return "", errors.New("got bogus message from STUN server")
	}
	resp := new(stun.Message)
	resp.Raw = buf
	err = resp.Decode()
	if err != nil {
		return "", err
	}
	var xorAddr stun.XORMappedAddress
	err = xorAddr.GetFrom(resp)
	if err != nil {
		return "", err
	}
	return xorAddr.String(), nil
}

// prefixStringWithLen: prefixes a string with its length, for use with ReadPrefixedStringWithLen
// fails if len(s) == 0
func prefixStringWithLen(s string) []byte {
	if len(s) == 0 {
		log.Fatal("Cannot prefix a string with length 0")
	}
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, uint32(len(s)))
	return append(buf, []byte(s)...)
}

// readLenPrefixedString: reads in a string from the reader assuming that the first 4 bytes
// are the length of the string
func readLenPrefixedString(r io.Reader) (string, error) {
	buf := make([]byte, 4)
	_, err := io.ReadFull(r, buf)
	if err != nil {
		return "", err
	}
	len := binary.BigEndian.Uint32(buf)

	ret := make([]byte, len)
	_, err = io.ReadFull(r, ret)
	if err != nil {
		return "", err
	}

	return string(ret), nil
}

// GenerateTLSConfig : Setup a bare-bones TLS config for the server
func GenerateTLSConfig() *tls.Config {
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		panic(err)
	}
	template := x509.Certificate{SerialNumber: big.NewInt(1)}
	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		panic(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		panic(err)
	}
	return &tls.Config{
		Certificates: []tls.Certificate{tlsCert},
		NextProtos:   []string{"quic-holepunch"},
	}
}
