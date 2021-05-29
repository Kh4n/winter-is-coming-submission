package swarm

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net"
	"os"
	"strconv"
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
func tryDialHolepunch(sessions chan<- quic.Session, conn *net.UDPConn, peerAddr string, tlsConf *tls.Config) {
	addr, err := net.ResolveUDPAddr("udp4", peerAddr)
	if err != nil {
		log.Fatal("Could not resolve address:", err)
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
		log.Println("Could not dial address:", err)
		sessions <- nil
		return
	}
	sessions <- sess
}

func sendPeerID(peerID string, dialSess quic.Session) (quic.Stream, error) {
	if dialSess == nil {
		return nil, fmt.Errorf("dialSess is nil")
	}
	dialStream, err := dialSess.OpenStream()
	if err != nil {
		return nil, fmt.Errorf("unable to open dial stream with peer: %s", err)
	}
	_, err = dialStream.Write(prefixStringWithLen(peerID))
	if err != nil {
		return nil, fmt.Errorf("unable to write to dial stream with peer: %s", err)
	}
	return dialStream, nil
}

func recvPeerID(listenSess quic.Session) (quic.Stream, string, error) {
	if listenSess == nil {
		return nil, "", fmt.Errorf("listenSess is nil")
	}
	listenStream, err := listenSess.AcceptStream(context.Background())
	if err != nil {
		return nil, "", fmt.Errorf("unable to accept listen stream with peer: %s", err)
	}
	remotePeerID, err := readLenPrefixedString(listenStream)
	if err != nil {
		return nil, "", fmt.Errorf("unable to read listen stream with peer: %s", err)
	}
	return listenStream, remotePeerID, nil
}

func syncPeerSessions(dialSess quic.Session, listenSess quic.Session) (quic.Session, quic.Stream, error) {
	peerID := strconv.FormatUint(rand.Uint64(), 16)
	dialStream, errDial := sendPeerID(peerID, dialSess)
	listenStream, remotePeerID, errListen := recvPeerID(listenSess)
	if errDial != nil && errListen != nil {
		return nil, nil, fmt.Errorf("unable to open dial stream or listen stream: %s %s", errDial, errListen)
	}
	if errDial == nil && errListen == nil {
		if dialSess.RemoteAddr().String() != listenSess.RemoteAddr().String() {
			return nil, nil, fmt.Errorf(
				"dial/listen remote addresses do not match: %s %s",
				dialSess.RemoteAddr(), listenSess.RemoteAddr(),
			)
		}
		if peerID > remotePeerID {
			listenStream.Close()
			listenSess.CloseWithError(ERR_PEER_INITIATOR, "Peer has decided it is the initiator")
			return dialSess, dialStream, nil
		} else {
			dialStream.Close()
			dialSess.CloseWithError(ERR_PEER_NOT_INITIATOR, "Peer has decided it is not the initiator")
			return listenSess, listenStream, nil
		}
	}
	if errDial != nil {
		return dialSess, dialStream, nil
	}
	if errListen != nil {
		return listenSess, dialStream, nil
	}
	log.Fatal("unknown case reached")
	return nil, nil, errors.New("unknown case reached")
}

// Will send msg to peer to prove connectivity. Gives up after timeout
func SimpleHolepunch(msg string, timeout time.Duration) error {
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
	inSessions := make(chan quic.Session)
	go handleIncomingSessions(inSessions, conn, tlsConf)
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("Enter remote peer address (with port): ")
		peerAddr, err := reader.ReadString('\n')
		if err != nil {
			return err
		}
		peerAddr = strings.Trim(peerAddr, "\n\r\t ")
		fmt.Println("Connecting to peer...")
		// both sides have to dial in order to holepunch
		// guaranteed connectivity requires you to be flexible
		// you must be able to handle cases where only one side connects (ie. Symmetric NAT <-> Normal NAT)
		outSessions := make(chan quic.Session, 1)
		go tryDialHolepunch(outSessions, conn, peerAddr, tlsConf)
		var dialSess, listenSess quic.Session
		giveup := time.After(timeout)
		// this is ugly ill admit, I do not know a better way
		// basically we are waiting for at least one to finish, barring the timeout
	outer:
		for i := 0; i < 2; i += 1 {
			select {
			case dialSess = <-outSessions:
				log.Println("Successfully dialed peer")
			case listenSess = <-inSessions:
				log.Println("Successfully received connection")
			case <-giveup:
				break outer
			}
		}
		sess, stream, err := syncPeerSessions(dialSess, listenSess)
		if err != nil {
			return err
		}
		fmt.Println("Session established with", sess.RemoteAddr())
		_, err = stream.Write(prefixStringWithLen(msg))
		if err != nil {
			return err
		}
		peerMsg, err := readLenPrefixedString(stream)
		if err != nil {
			return err
		}
		fmt.Println("Got message from peer:", peerMsg)
	}
}
