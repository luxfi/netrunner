package local

import (
	"bytes"
	"context"
	"crypto"
	"crypto/tls"
	"encoding/binary"
	"io"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/luxfi/netrunner/network/node"
	"github.com/luxfi/node/ids"
	"github.com/luxfi/node/message"
	"github.com/luxfi/node/network/peer"
	"github.com/luxfi/node/staking"
	"github.com/luxfi/node/utils/constants"
	"github.com/luxfi/node/utils/ips"
	"github.com/luxfi/node/utils/logging"
	"github.com/luxfi/node/utils/wrappers"
	"github.com/luxfi/node/version"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

const bitmaskCodec = uint32(1 << 31)

func upgradeConn(myTLSCert *tls.Certificate, conn net.Conn) (ids.NodeID, net.Conn, error) {
	tlsConfig := peer.TLSConfig(*myTLSCert, nil)
	upgrader := peer.NewTLSServerUpgrader(tlsConfig)
	// this will block until the ssh handshake is done
	peerID, tlsConn, _, err := upgrader.Upgrade(conn)
	return peerID, tlsConn, err
}

// verifyProtocol reads from the connection and asserts that we read the expected message sequence.
// It also sends the required messages to complete the p2p handshake.
// Sequence:
// 1. Write the version message length to peer
// 2. Write version message to peer
// 3. Write peerlist message length to peer
// 4. Write peerlist message to peer
// If an unexpected error occurs, or we get an unexpected message, sends an error on [errCh].
// Sends nil on [errCh] if we get the expected message sequence.
func verifyProtocol(
	require *require.Assertions,
	opSequence []message.Op,
	mc message.Creator,
	nodeConn net.Conn,
	errCh chan error,
) {
	// do the TLS handshake
	myTLSCert, err := staking.NewTLSCert()
	if err != nil {
		errCh <- err
		return
	}
	peerID, tlsConn, err := upgradeConn(myTLSCert, nodeConn)
	if err != nil {
		errCh <- err
		return
	}
	nodeConn = tlsConn

	// send the peer our version and peerlist

	// create the version message
	myIP := netip.AddrPortFrom(netip.IPv6Unspecified(), 0)
	now := uint64(time.Now().Unix())
	unsignedIP := peer.UnsignedIP{
		AddrPort:  myIP,
		Timestamp: now,
	}
	signer := myTLSCert.PrivateKey.(crypto.Signer)
	signedIP, err := unsignedIP.Sign(signer)
	if err != nil {
		errCh <- err
		return
	}
	verMsg, err := mc.Version(
		constants.MainnetID,
		now,
		myIP,
		version.CurrentApp.String(),
		now,
		signedIP.Signature,
		[]ids.ID{},
	)
	if err != nil {
		errCh <- err
		return
	}

	// create the PeerList message
	plMsg, err := mc.PeerList([]ips.ClaimedIPPort{}, true)
	if err != nil {
		errCh <- err
		return
	}

	// send the Version message
	if err := sendMessage(nodeConn, verMsg.Bytes(), errCh); err != nil {
		// if there was an error no need to continue
		return
	}
	// send the PeerList message
	if err := sendMessage(nodeConn, plMsg.Bytes(), errCh); err != nil {
		// if there was an error no need to continue
		return
	}

	// at this point we sent all messages expected for handshake,
	// now *read* the messages on the other end and check they are in
	// the expected sequence
	for _, expectedOpMsg := range opSequence {
		msgBytes, err := readMessage(nodeConn, errCh)
		if err != nil {
			// If there was an error no need continue
			return
		}
		msg, err := mc.Parse(msgBytes.Bytes(), peerID, func() {})
		require.NoError(err)
		op := msg.Op()
		require.Equal(expectedOpMsg, op)
	}
	// signal we are actually done
	errCh <- nil
}

// readMessage reads from the connection and returns a protocol message in bytes
func readMessage(nodeConn net.Conn, errCh chan error) (*bytes.Buffer, error) {
	msgLenBytes := &bytes.Buffer{}
	// read the message length
	if _, err := io.CopyN(msgLenBytes, nodeConn, wrappers.IntLen); err != nil {
		errCh <- err
		return nil, err
	}
	msgLen := binary.BigEndian.Uint32(msgLenBytes.Bytes())
	msgLen &^= bitmaskCodec
	msgBytes := &bytes.Buffer{}
	// read the message
	if _, err := io.CopyN(msgBytes, nodeConn, int64(msgLen)); err != nil {
		errCh <- err
		return nil, err
	}
	return msgBytes, nil
}

// sendMessage sends a protocol message to the node peer
func sendMessage(nodeConn net.Conn, msgBytes []byte, errCh chan error) error {
	// buffer for message length
	msgLenBytes := make([]byte, wrappers.IntLen)
	lenBuf := bytes.NewBuffer(msgLenBytes)

	// write the message length
	binary.BigEndian.PutUint32(msgLenBytes, uint32(len(msgBytes)))
	// send the message length
	if _, err := io.CopyN(nodeConn, lenBuf, wrappers.IntLen); err != nil {
		errCh <- err
		return err
	}
	// write the message
	msgBuf := bytes.NewBuffer(msgBytes)
	// send the message
	if _, err := io.CopyN(nodeConn, msgBuf, int64(len(msgBytes))); err != nil {
		errCh <- err
		return err
	}
	return nil
}

// TestAttachPeer tests that we can attach a test peer to a node
// and that the node receives messages sent through the test peer
func TestAttachPeer(t *testing.T) {
	require := require.New(t)

	// [nodeConn] is the connection that [node] uses to read from/write to [peer] (defined below)
	// Similar for [peerConn].
	nodeConn, peerConn := net.Pipe()
	defer func() {
		_ = nodeConn.Close()
		_ = peerConn.Close()
	}()

	node := localNode{
		nodeID:    ids.GenerateTestNodeID(),
		networkID: constants.MainnetID,
		getConnFunc: func(ctx context.Context, n node.Node) (net.Conn, error) {
			return peerConn, nil
		},
		attachedPeers: map[string]peer.Peer{},
	}

	// For message creation and parsing
	mc, err := message.NewCreator(
		logging.NoLog{},
		prometheus.NewRegistry(),
		"",
		constants.DefaultNetworkCompressionType,
		10*time.Second,
	)
	require.NoError(err)

	// Expect the peer to send these messages in this order.
	expectedMessages := []message.Op{
		message.VersionOp,
		message.PeerListOp,
		message.ChitsOp,
	}

	// [p] define below will write to/read from [peerConn]
	// Start a goroutine that reads messages from the other end of that
	// connection and asserts that we get the expected messages
	errCh := make(chan error, 1)
	go verifyProtocol(require, expectedMessages, mc, nodeConn, errCh)

	// attach a test peer to [node]
	handler := &noOpInboundHandler{}
	p, err := node.AttachPeer(context.Background(), handler)
	require.NoError(err)

	// we'll use a Chits message for testing. (We could use any message type.)
	containerIDs := []ids.ID{
		ids.GenerateTestID(),
		ids.GenerateTestID(),
		ids.GenerateTestID(),
	}
	requestID := uint32(42)
	chainID := constants.PlatformChainID
	// create the Chits message
	msg, err := mc.Chits(chainID, requestID, []ids.ID{}, containerIDs)
	require.NoError(err)
	// send chits to [node]
	ok := p.Send(context.Background(), msg)
	require.True(ok)
	// wait until the go routines are done
	// also ensures that [require] calls will be reflected in test results if failed
	require.NoError(<-errCh)
}
