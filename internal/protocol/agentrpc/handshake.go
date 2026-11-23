package agentrpc

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"

	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/protobuf/proto"

	"github.com/kakkky/gorviz/internal/domain/observation"
)

const maxHandshakeBytes = 4 << 10

// HandshakeInfo は server が接続ごとに受け取ったハンドシェイク。interceptor がトークンを確かめるのに使う
type HandshakeInfo struct {
	credentials.CommonAuthInfo
	base    credentials.AuthInfo
	AgentID observation.AgentID
	Token   string
}

func (h HandshakeInfo) AuthType() string {
	if h.base != nil {
		return h.base.AuthType() + "+gorviz"
	}
	return "gorviz"
}

func HandshakeFromContext(ctx context.Context) (HandshakeInfo, bool) {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return HandshakeInfo{}, false
	}
	h, ok := p.AuthInfo.(HandshakeInfo)
	return h, ok
}

// handshakeCredentials は base (TLS か平文) で暗号化した直後に、agent の起動ごとの ID とトークンを送る / 受ける
type handshakeCredentials struct {
	credentials.TransportCredentials
	send *Handshake
}

// NewClientCredentials は agent が使う
func NewClientCredentials(base credentials.TransportCredentials, agent observation.AgentID, token string) credentials.TransportCredentials {
	return &handshakeCredentials{TransportCredentials: base, send: &Handshake{AgentId: string(agent), Token: token}}
}

// NewServerCredentials は server が使う
func NewServerCredentials(base credentials.TransportCredentials) credentials.TransportCredentials {
	return &handshakeCredentials{TransportCredentials: base}
}

func (c *handshakeCredentials) ClientHandshake(ctx context.Context, authority string, raw net.Conn) (net.Conn, credentials.AuthInfo, error) {
	conn, info, err := c.TransportCredentials.ClientHandshake(ctx, authority, raw)
	if err != nil {
		return nil, nil, err
	}
	if err := writeHandshake(conn, c.send); err != nil {
		conn.Close()
		return nil, nil, err
	}
	return conn, info, nil
}

func (c *handshakeCredentials) ServerHandshake(raw net.Conn) (net.Conn, credentials.AuthInfo, error) {
	conn, info, err := c.TransportCredentials.ServerHandshake(raw)
	if err != nil {
		return nil, nil, err
	}
	h, err := readHandshake(conn)
	if err != nil {
		conn.Close()
		return nil, nil, err
	}
	hi := HandshakeInfo{CommonAuthInfo: credentials.CommonAuthInfo{SecurityLevel: credentials.NoSecurity}, base: info, AgentID: observation.AgentID(h.GetAgentId()), Token: h.GetToken()}
	if ci, ok := info.(interface {
		GetCommonAuthInfo() credentials.CommonAuthInfo
	}); ok {
		hi.CommonAuthInfo = ci.GetCommonAuthInfo()
	}
	return conn, hi, nil
}

func (c *handshakeCredentials) Clone() credentials.TransportCredentials {
	return &handshakeCredentials{TransportCredentials: c.TransportCredentials.Clone(), send: c.send}
}

func writeHandshake(w io.Writer, h *Handshake) error {
	b, err := proto.Marshal(h)
	if err != nil {
		return err
	}
	buf := binary.AppendUvarint(nil, uint64(len(b)))
	_, err = w.Write(append(buf, b...))
	return err
}

func readHandshake(r io.Reader) (*Handshake, error) {
	n, err := binary.ReadUvarint(byteReader{r})
	if err != nil {
		return nil, err
	}
	if n > maxHandshakeBytes {
		return nil, errors.New("the handshake is too long")
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(r, b); err != nil {
		return nil, err
	}
	var h Handshake
	if err := proto.Unmarshal(b, &h); err != nil {
		return nil, err
	}
	return &h, nil
}

// byteReader は 1 バイトずつ読む。後ろは gRPC がそのまま使うので、余分に読み進めない
type byteReader struct{ r io.Reader }

func (b byteReader) ReadByte() (byte, error) {
	var buf [1]byte
	_, err := io.ReadFull(b.r, buf[:])
	return buf[0], err
}
