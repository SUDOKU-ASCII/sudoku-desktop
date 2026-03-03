package core

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand"
	"net"
	"strings"
	"time"
)

func healthCheckSystemTCPAny(ctx context.Context, targets []string, timeout time.Duration) error {
	if len(targets) == 0 {
		return errors.New("no targets")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	d := &net.Dialer{Timeout: timeout}
	var errs []string
	for _, t := range targets {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		c, err := d.DialContext(ctx, "tcp", t)
		if err == nil {
			_ = c.Close()
			return nil
		}
		errs = append(errs, t+": "+err.Error())
	}
	if len(errs) == 0 {
		return errors.New("no valid targets")
	}
	return errors.New(strings.Join(errs, " | "))
}

func healthCheckSOCKS5Connect(ctx context.Context, socksAddr string, target string, timeout time.Duration) error {
	socksAddr = strings.TrimSpace(socksAddr)
	target = strings.TrimSpace(target)
	if socksAddr == "" || target == "" {
		return errors.New("empty socksAddr/target")
	}
	h, p, err := net.SplitHostPort(target)
	if err != nil {
		return err
	}
	ip := net.ParseIP(strings.TrimSpace(h))
	if ip == nil {
		return fmt.Errorf("target must be ip:port, got %q", target)
	}
	port, err := net.LookupPort("tcp", p)
	if err != nil {
		return err
	}
	if port <= 0 || port > 65535 {
		return fmt.Errorf("invalid target port: %d", port)
	}

	dialer := &net.Dialer{Timeout: timeout}
	c, err := dialer.DialContext(ctx, "tcp", socksAddr)
	if err != nil {
		return err
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(timeout))

	// Greeting: VER=5, NMETHODS=1, METHOD=0(no auth)
	if _, err := c.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		return err
	}
	var rep [2]byte
	if _, err := ioReadFull(c, rep[:]); err != nil {
		return err
	}
	if rep[0] != 0x05 || rep[1] != 0x00 {
		return fmt.Errorf("socks5 auth negotiate failed: %v", rep)
	}

	var req []byte
	if ip4 := ip.To4(); ip4 != nil {
		req = make([]byte, 4+4+2)
		req[0] = 0x05
		req[1] = 0x01 // CONNECT
		req[2] = 0x00
		req[3] = 0x01 // IPv4
		copy(req[4:8], ip4)
		binary.BigEndian.PutUint16(req[8:10], uint16(port))
	} else if ip16 := ip.To16(); ip16 != nil {
		req = make([]byte, 4+16+2)
		req[0] = 0x05
		req[1] = 0x01 // CONNECT
		req[2] = 0x00
		req[3] = 0x04 // IPv6
		copy(req[4:20], ip16)
		binary.BigEndian.PutUint16(req[20:22], uint16(port))
	} else {
		return fmt.Errorf("invalid ip: %q", ip.String())
	}
	if _, err := c.Write(req); err != nil {
		return err
	}

	// Reply: VER, REP, RSV, ATYP, BND.ADDR, BND.PORT
	var hdr [4]byte
	if _, err := ioReadFull(c, hdr[:]); err != nil {
		return err
	}
	if hdr[0] != 0x05 {
		return fmt.Errorf("invalid socks5 reply ver: %d", hdr[0])
	}
	if hdr[1] != 0x00 {
		return fmt.Errorf("socks5 connect failed: rep=%d", hdr[1])
	}
	atyp := hdr[3]
	switch atyp {
	case 0x01: // IPv4
		if _, err := ioReadFull(c, make([]byte, 4+2)); err != nil {
			return err
		}
	case 0x04: // IPv6
		if _, err := ioReadFull(c, make([]byte, 16+2)); err != nil {
			return err
		}
	case 0x03: // DOMAIN
		var l [1]byte
		if _, err := ioReadFull(c, l[:]); err != nil {
			return err
		}
		if _, err := ioReadFull(c, make([]byte, int(l[0])+2)); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unexpected socks5 atyp: %d", atyp)
	}
	return nil
}

func healthCheckSOCKS5ConnectAny(ctx context.Context, socksAddr string, targets []string, timeout time.Duration) error {
	if len(targets) == 0 {
		return errors.New("no targets")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	var errs []string
	for _, t := range targets {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		if err := healthCheckSOCKS5Connect(ctx, socksAddr, t, timeout); err == nil {
			return nil
		} else {
			errs = append(errs, t+": "+err.Error())
		}
	}
	if len(errs) == 0 {
		return errors.New("no valid targets")
	}
	return errors.New(strings.Join(errs, " | "))
}

func healthCheckDNSUDP(ctx context.Context, server string, qname string, timeout time.Duration) error {
	server = strings.TrimSpace(server)
	qname = strings.TrimSpace(qname)
	if server == "" || qname == "" {
		return errors.New("empty dns server/qname")
	}
	if !strings.Contains(server, ":") {
		server = net.JoinHostPort(server, "53")
	}

	id := uint16(rand.New(rand.NewSource(time.Now().UnixNano())).Uint32())
	req := buildDNSQuery(id, qname)

	d := &net.Dialer{Timeout: timeout}
	c, err := d.DialContext(ctx, "udp", server)
	if err != nil {
		return err
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(timeout))
	if _, err := c.Write(req); err != nil {
		return err
	}
	buf := make([]byte, 1500)
	n, err := c.Read(buf)
	if err != nil {
		return err
	}
	if n < 12 {
		return errors.New("short dns response")
	}
	if binary.BigEndian.Uint16(buf[0:2]) != id {
		return errors.New("dns id mismatch")
	}
	ancount := binary.BigEndian.Uint16(buf[6:8])
	if ancount == 0 {
		return errors.New("dns answer empty")
	}
	return nil
}

func buildDNSQuery(id uint16, qname string) []byte {
	// Minimal DNS query for A record, IN class.
	// Header: ID, flags(0x0100), QDCOUNT=1.
	h := make([]byte, 12)
	binary.BigEndian.PutUint16(h[0:2], id)
	binary.BigEndian.PutUint16(h[2:4], 0x0100)
	binary.BigEndian.PutUint16(h[4:6], 1)

	var q []byte
	for _, label := range strings.Split(qname, ".") {
		label = strings.TrimSpace(label)
		if label == "" {
			continue
		}
		if len(label) > 63 {
			break
		}
		q = append(q, byte(len(label)))
		q = append(q, []byte(label)...)
	}
	q = append(q, 0x00)       // end
	q = append(q, 0x00, 0x01) // QTYPE A
	q = append(q, 0x00, 0x01) // QCLASS IN
	return append(h, q...)
}

func ioReadFull(c net.Conn, b []byte) (int, error) {
	n := 0
	for n < len(b) {
		m, err := c.Read(b[n:])
		if m > 0 {
			n += m
		}
		if err != nil {
			return n, err
		}
	}
	return n, nil
}
