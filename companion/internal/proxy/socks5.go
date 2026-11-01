package proxy

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strconv"
)

const (
	socksVersion = 5
	authNone     = 0
	authReject   = 0xff

	cmdConnect = 0x01

	atypIPv4   = 0x01
	atypDomain = 0x03
	atypIPv6   = 0x04

	replySuccess             = 0x00
	replyGeneralFailure      = 0x01
	replyCommandNotSupported = 0x07
	replyAddressNotSupported = 0x08
	maxDomainBytes           = 255
)

var errBadHandshake = errors.New("proxy: invalid socks5 handshake")

// readConnect performs a no-auth SOCKS5 CONNECT negotiation and returns the
// destination as host:port. Domain names are passed through unresolved.
func readConnect(conn net.Conn) (string, error) {
	var greeting [2]byte
	if _, err := io.ReadFull(conn, greeting[:]); err != nil {
		return "", err
	}
	if greeting[0] != socksVersion {
		return "", errBadHandshake
	}
	methods := make([]byte, int(greeting[1]))
	if _, err := io.ReadFull(conn, methods); err != nil {
		return "", err
	}
	if bytes.IndexByte(methods, authNone) < 0 {
		_ = writeMethod(conn, authReject)
		return "", errBadHandshake
	}
	if err := writeMethod(conn, authNone); err != nil {
		return "", err
	}

	var request [4]byte
	if _, err := io.ReadFull(conn, request[:]); err != nil {
		return "", err
	}
	if request[0] != socksVersion || request[2] != 0 {
		return "", errBadHandshake
	}
	if request[1] != cmdConnect {
		_ = writeReply(conn, replyCommandNotSupported)
		return "", errBadHandshake
	}

	host, err := readAddress(conn, request[3])
	if err != nil {
		_ = writeReply(conn, replyAddressNotSupported)
		return "", err
	}
	var portBytes [2]byte
	if _, err := io.ReadFull(conn, portBytes[:]); err != nil {
		return "", err
	}
	port := int(binary.BigEndian.Uint16(portBytes[:]))
	if port == 0 {
		_ = writeReply(conn, replyGeneralFailure)
		return "", errBadHandshake
	}
	return net.JoinHostPort(host, strconv.Itoa(port)), nil
}

func readAddress(r io.Reader, atyp byte) (string, error) {
	switch atyp {
	case atypIPv4:
		ip := make(net.IP, net.IPv4len)
		if _, err := io.ReadFull(r, ip); err != nil {
			return "", err
		}
		return ip.String(), nil
	case atypIPv6:
		ip := make(net.IP, net.IPv6len)
		if _, err := io.ReadFull(r, ip); err != nil {
			return "", err
		}
		return ip.String(), nil
	case atypDomain:
		var length [1]byte
		if _, err := io.ReadFull(r, length[:]); err != nil {
			return "", err
		}
		size := int(length[0])
		if size == 0 || size > maxDomainBytes {
			return "", errBadHandshake
		}
		name := make([]byte, size)
		if _, err := io.ReadFull(r, name); err != nil {
			return "", err
		}
		if !validDomainBytes(name) {
			return "", errBadHandshake
		}
		return string(name), nil
	default:
		return "", errBadHandshake
	}
}

// validDomainBytes rejects bytes that cannot be part of a hostname and would
// make the host:port join ambiguous. Non-ASCII UTF-8 is left to the tunnel's
// resolver.
func validDomainBytes(name []byte) bool {
	for _, b := range name {
		if b <= 0x20 || b == 0x7f || b == ':' || b == '/' || b == '@' || b == '[' || b == ']' || b == '%' || b == '\\' {
			return false
		}
	}
	return true
}

func writeMethod(conn net.Conn, method byte) error {
	return writeAll(conn, []byte{socksVersion, method})
}

func writeReply(conn net.Conn, code byte) error {
	return writeAll(conn, []byte{socksVersion, code, 0, atypIPv4, 0, 0, 0, 0, 0, 0})
}

func writeAll(w io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := w.Write(data)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}
