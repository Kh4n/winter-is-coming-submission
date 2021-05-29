package swarm

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"time"

	quic "github.com/lucas-clemente/quic-go"
)

const (
	ERR_PEER_LEFT          quic.ErrorCode = iota
	ERR_PEER_ADDR_RECEIVED quic.ErrorCode = iota
	ERR_PEER_ADDR_INVALID  quic.ErrorCode = iota
	ERR_PEER_INITIATOR     quic.ErrorCode = iota
	ERR_PEER_NOT_INITIATOR quic.ErrorCode = iota
)

// Nab a random udp port
func setupRandomUDP() (*net.UDPConn, error) {
	listenAddr, err := net.ResolveUDPAddr("udp4", ":0")
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp4", listenAddr)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

// Listens on the connection for incoming QUIC sessions, and pushes them to sessions channel
// You are only allowed to call quic.Listen once per connection, so this function should only be called once
func handleIncomingSessions(sessions chan<- quic.Session, conn *net.UDPConn, tlsConf *tls.Config) {
	listener, err := quic.Listen(conn, GenerateTLSConfig(), &quic.Config{KeepAlive: true})
	if err != nil {
		log.Fatalf("Error listening: %s", err)
		return
	}
	for {
		sess, err := listener.Accept(context.Background())
		if err != nil {
			log.Printf("Error accepting session: %s\n", err)
			continue
		}
		sessions <- sess
	}
}

// Dials peerAddr, which initiates the holepunch
func tryDialHolepunch(conn *net.UDPConn, peerAddr string, tlsConf *tls.Config) (quic.Session, error) {
	addr, err := net.ResolveUDPAddr("udp4", peerAddr)
	if err != nil {
		return nil, err
	}
	var (
		sess quic.Session = nil
	)
	for i := 0; i < 10; i += 1 {
		sess, err = quic.Dial(conn, addr, peerAddr, tlsConf, &quic.Config{KeepAlive: true})
		if err == nil {
			break
		}
		log.Println("Unable to dial, retrying:", err)
		time.Sleep(200 * time.Millisecond)
	}
	if err != nil {
		return nil, err
	}
	return sess, nil
}

// Will send msg to peer to prove connectivity
func SimpleHolepunch(msg string) error {
	conn, err := setupRandomUDP()
	if err != nil {
		return err
	}
	// print local address to observe basic info about NAT
	fmt.Println("Local address:", conn.LocalAddr())
	// get our public IP and port for this specific connection from the stun server
	ourAddr, err := addrFromStun(conn)
	if err != nil {
		return err
	}
	fmt.Println("Your address is:", ourAddr)

	tlsConf := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{"quic-holepunch"},
	}
	sessions := make(chan quic.Session)
	go handleIncomingSessions(sessions, conn, tlsConf)
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("Enter remote peer address (with port): ")
		addr, err := reader.ReadString('\n')
		if err != nil {
			return err
		}
		addr = strings.Trim(addr, "\n\r\t ")
		fmt.Println("Connecting to peer...")
		// both sides have to dial in order to holepunch
		// there are two options for connectivity
		// 1. open two sessions and keep both
		// 2. only keep one, the peers need to sync up and choose which to keep
		// the first method is simpler, but possibly wasteful
		// the second is more complex but keeps everything in one session
		// I have gone with the first option in an effort to keep code simple
		dialSess, err := tryDialHolepunch(conn, addr, tlsConf)
		if err != nil {
			return err
		}
		outStream, err := dialSess.OpenUniStream()
		if err != nil {
			return err
		}
		_, err = outStream.Write(prefixStringWithLen(msg))
		if err != nil {
			return err
		}
		listenSess := <-sessions
		fmt.Println("Session established with", addr)
		inStream, err := listenSess.AcceptUniStream(context.Background())
		if err != nil {
			return err
		}
		// ultra simple protocol: length prefixed strings
		peerMsg, err := readLenPrefixedString(inStream)
		if err != nil {
			return err
		}
		fmt.Println("Got message from peer:", peerMsg)
	}
}
