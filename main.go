package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

const (
	proxySOCKS5 = "socks5"
	proxyHTTP   = "http"

	socksVersion = 0x05
	authNoAuth   = 0x00
	cmdConnect   = 0x01

	atypIPv4   = 0x01
	atypDomain = 0x03
	atypIPv6   = 0x04

	replySuccess = 0x00

	connectTimeout = 15 * time.Second

	sniReadTimeout     = 10 * time.Second
	maxTLSRecordSize   = (1 << 14) + 2048
	maxClientHelloSize = 1 << 20
)

type Config struct {
	Listen    string
	Target    string
	ProxyType string

	SOCKS5Addr string

	HTTPProxyAddr string
	HTTPUser      string
	HTTPPass      string
}

type TrafficStats struct {
	ClientToServer int64
	ServerToClient int64
}

type ProxyDialer interface {
	Connect(target string) (net.Conn, io.Reader, error)
}

var (
	connectionID uint64
	activeConns  sync.Map
)

func main() {
	cfg := Config{}

	flag.StringVar(
		&cfg.Listen,
		"listen",
		":443",
		"Local TCP listen address",
	)

	flag.StringVar(
		&cfg.Target,
		"target",
		"example.com:443",
		"Default target host:port",
	)

	flag.StringVar(
		&cfg.ProxyType,
		"proxy-type",
		"socks5",
		"Remote proxy type: socks5 or http",
	)

	flag.StringVar(
		&cfg.SOCKS5Addr,
		"socks5",
		"127.0.0.1:1080",
		"Remote SOCKS5 proxy address",
	)

	flag.StringVar(
		&cfg.HTTPProxyAddr,
		"http-proxy",
		"127.0.0.1:7890",
		"Remote HTTP proxy address",
	)

	flag.StringVar(
		&cfg.HTTPUser,
		"http-user",
		"",
		"HTTP proxy username",
	)

	flag.StringVar(
		&cfg.HTTPPass,
		"http-pass",
		"",
		"HTTP proxy password",
	)

	flag.Parse()

	log.SetFlags(
		log.Ldate |
			log.Ltime |
			log.Lmicroseconds,
	)

	if err := validateConfig(&cfg); err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	log.Println("================================================")
	log.Println("TCP HTTPS Forward Proxy")
	log.Println("================================================")
	log.Printf("Local Listen : %s", cfg.Listen)
	log.Printf("Target       : %s", cfg.Target)
	log.Printf("Proxy Type   : %s", cfg.ProxyType)

	if cfg.ProxyType == proxySOCKS5 {
		log.Printf("SOCKS5 Proxy : %s", cfg.SOCKS5Addr)
	} else {
		log.Printf("HTTP Proxy   : %s", cfg.HTTPProxyAddr)
		if cfg.HTTPUser != "" {
			log.Printf("HTTP Auth    : enabled")
		}
	}

	log.Println("================================================")

	if err := run(cfg); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

// ============================================================
// Configuration
// ============================================================

func validateConfig(cfg *Config) error {
	if cfg.ProxyType != proxySOCKS5 &&
		cfg.ProxyType != proxyHTTP {
		return fmt.Errorf(
			"invalid proxy type %q, use socks5 or http",
			cfg.ProxyType,
		)
	}

	if _, _, err := net.SplitHostPort(cfg.Target); err != nil {
		return fmt.Errorf("invalid target %q: %w", cfg.Target, err)
	}

	host, port, _ := net.SplitHostPort(cfg.Target)

	if host == "" {
		return errors.New("target host cannot be empty")
	}

	if err := validatePort(port); err != nil {
		return fmt.Errorf("invalid target port: %w", err)
	}

	if _, _, err := net.SplitHostPort(cfg.Listen); err != nil {
		return fmt.Errorf("invalid listen address: %w", err)
	}

	if cfg.ProxyType == proxySOCKS5 {
		if err := validateProxyAddress(cfg.SOCKS5Addr); err != nil {
			return fmt.Errorf("invalid SOCKS5 address: %w", err)
		}
	}

	if cfg.ProxyType == proxyHTTP {
		if err := validateProxyAddress(cfg.HTTPProxyAddr); err != nil {
			return fmt.Errorf("invalid HTTP proxy address: %w", err)
		}
	}

	if cfg.HTTPUser == "" && cfg.HTTPPass != "" {
		return errors.New("HTTP password requires HTTP username")
	}

	return nil
}

func validatePort(port string) error {
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return fmt.Errorf("port out of range: %s", port)
	}
	return nil
}

func validateProxyAddress(address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}

	if host == "" {
		return errors.New("proxy host cannot be empty")
	}

	return validatePort(port)
}

// ============================================================
// Proxy Dialer Factory
// ============================================================

func newProxyDialer(cfg Config) ProxyDialer {
	switch cfg.ProxyType {
	case proxySOCKS5:
		return &SOCKS5Dialer{
			Address: cfg.SOCKS5Addr,
		}

	case proxyHTTP:
		return &HTTPProxyDialer{
			Address:  cfg.HTTPProxyAddr,
			Username: cfg.HTTPUser,
			Password: cfg.HTTPPass,
		}
	}

	return nil
}

// ============================================================
// Main TCP Server
// ============================================================

func run(cfg Config) error {
	listener, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return fmt.Errorf("listen failed: %w", err)
	}

	log.Printf("[SERVER] Listening on %s", cfg.Listen)

	dialer := newProxyDialer(cfg)
	if dialer == nil {
		listener.Close()
		return errors.New("cannot create proxy dialer")
	}

	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	// Close listener when receiving shutdown signal.
	go func() {
		<-ctx.Done()

		log.Println("[SERVER] Shutdown signal received")

		_ = listener.Close()

		activeConns.Range(func(key, value any) bool {
			if conn, ok := value.(net.Conn); ok {
				_ = conn.Close()
			}
			return true
		})
	}()

	for {
		client, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				break
			}

			if ne, ok := err.(net.Error); ok && ne.Temporary() {
				log.Printf("[SERVER] Temporary accept error: %v", err)
				time.Sleep(100 * time.Millisecond)
				continue
			}

			log.Printf("[SERVER] Accept error: %v", err)
			continue
		}

		go handleConnection(client, cfg, dialer)
	}

	log.Println("[SERVER] Waiting for connections to finish")

	return nil
}

// ============================================================
// Handle Client Connection
// ============================================================

func handleConnection(
	client net.Conn,
	cfg Config,
	dialer ProxyDialer,
) {
	id := atomic.AddUint64(&connectionID, 1)

	start := time.Now()
	clientAddr := client.RemoteAddr().String()

	activeConns.Store(id, client)

	defer func() {
		activeConns.Delete(id)
		_ = client.Close()
	}()

	// Read and cache the TLS ClientHello before opening the remote tunnel.
	// The cached bytes are forwarded unchanged after the proxy CONNECT succeeds.
	if err := client.SetReadDeadline(time.Now().Add(sniReadTimeout)); err != nil {
		log.Printf("[SNI][%d] cannot set read deadline: %v", id, err)
		return
	}

	sni, prefetched, sniErr := readClientHelloSNI(client)
	if err := client.SetReadDeadline(time.Time{}); err != nil {
		log.Printf("[SNI][%d] cannot clear read deadline: %v", id, err)
		return
	}

	target := cfg.Target
	if sni != "" {
		_, port, _ := net.SplitHostPort(cfg.Target)
		target = net.JoinHostPort(sni, port)
		log.Printf("[SNI][%d] detected server_name=%s target=%s", id, sni, target)
	} else {
		if sniErr != nil {
			log.Printf("[SNI][%d] detection unavailable, fallback target=%s reason=%v", id, target, sniErr)
		} else {
			log.Printf("[SNI][%d] no server_name, fallback target=%s", id, target)
		}
	}

	log.Printf(
		"[CONN][%d] CONNECT client=%s target=%s proxy=%s time=%s",
		id,
		clientAddr,
		target,
		cfg.ProxyType,
		start.Format(time.RFC3339Nano),
	)

	// Establish remote proxy tunnel before forwarding data.
	remote, remoteReader, err := dialer.Connect(target)
	if err != nil {
		log.Printf(
			"[CONN][%d] PROXY CONNECT FAILED client=%s error=%v",
			id,
			clientAddr,
			err,
		)
		return
	}

	activeConns.Store(
		fmt.Sprintf("%d-remote", id),
		remote,
	)

	defer func() {
		activeConns.Delete(fmt.Sprintf("%d-remote", id))
		_ = remote.Close()
	}()

	established := time.Now()

	log.Printf(
		"[CONN][%d] ESTABLISHED client=%s target=%s elapsed=%s time=%s",
		id,
		clientAddr,
		target,
		established.Sub(start),
		established.Format(time.RFC3339Nano),
	)

	// Start bidirectional transparent forwarding.
	forward(
		id,
		client,
		remote,
		io.MultiReader(bytes.NewReader(prefetched), client),
		remoteReader,
		target,
		start,
		established,
	)
}

// ============================================================
// TLS ClientHello / SNI Detection
// ============================================================

// readClientHelloSNI reads enough TLS records to obtain the first ClientHello.
// Every byte read from conn is returned in prefetched so it can be forwarded
// unchanged, even when detection fails or the client sends a non-TLS protocol.
func readClientHelloSNI(conn net.Conn) (string, []byte, error) {
	var prefetched []byte
	var handshake []byte

	for len(prefetched) < maxClientHelloSize {
		header := make([]byte, 5)
		n, err := io.ReadFull(conn, header)
		prefetched = append(prefetched, header[:n]...)
		if err != nil {
			return "", prefetched, fmt.Errorf("read TLS record header: %w", err)
		}

		// TLS Handshake record. If this is not TLS, preserve the bytes and
		// fall back to the configured target.
		if header[0] != 0x16 {
			return "", prefetched, fmt.Errorf("first record is not a TLS handshake")
		}

		recordLen := int(binary.BigEndian.Uint16(header[3:5]))
		if recordLen == 0 || recordLen > maxTLSRecordSize {
			return "", prefetched, fmt.Errorf("invalid TLS record length: %d", recordLen)
		}

		payload := make([]byte, recordLen)
		n, err = io.ReadFull(conn, payload)
		prefetched = append(prefetched, payload[:n]...)
		if err != nil {
			return "", prefetched, fmt.Errorf("read TLS record payload: %w", err)
		}

		handshake = append(handshake, payload...)
		if len(handshake) > maxClientHelloSize {
			return "", prefetched, errors.New("TLS ClientHello exceeds size limit")
		}

		if len(handshake) >= 1 && handshake[0] != 0x01 {
			return "", prefetched, fmt.Errorf("first TLS handshake message is not ClientHello")
		}

		if len(handshake) >= 4 {
			helloLen := int(handshake[1])<<16 |
				int(handshake[2])<<8 |
				int(handshake[3])
			totalLen := 4 + helloLen
			if totalLen > maxClientHelloSize {
				return "", prefetched, errors.New("TLS ClientHello exceeds size limit")
			}
			if len(handshake) >= totalLen {
				return parseClientHelloSNI(handshake[4:totalLen], prefetched)
			}
		}
	}

	return "", prefetched, errors.New("TLS ClientHello exceeds size limit")
}

// parseClientHelloSNI parses the ClientHello body without performing a TLS
// handshake. All offsets and lengths are checked before slicing.
func parseClientHelloSNI(body, prefetched []byte) (string, []byte, error) {
	offset := 0
	readBytes := func(n int) ([]byte, error) {
		if n < 0 || offset+n > len(body) {
			return nil, errors.New("malformed TLS ClientHello: truncated field")
		}
		value := body[offset : offset+n]
		offset += n
		return value, nil
	}

	if _, err := readBytes(2 + 32); err != nil { // legacy_version + random
		return "", prefetched, err
	}

	field, err := readBytes(1)
	if err != nil {
		return "", prefetched, err
	}
	if _, err = readBytes(int(field[0])); err != nil { // session_id
		return "", prefetched, err
	}

	field, err = readBytes(2)
	if err != nil {
		return "", prefetched, err
	}
	cipherLen := int(binary.BigEndian.Uint16(field))
	if cipherLen == 0 || cipherLen%2 != 0 {
		return "", prefetched, errors.New("malformed TLS ClientHello: invalid cipher suite length")
	}
	if _, err = readBytes(cipherLen); err != nil {
		return "", prefetched, err
	}

	field, err = readBytes(1)
	if err != nil {
		return "", prefetched, err
	}
	if _, err = readBytes(int(field[0])); err != nil { // compression methods
		return "", prefetched, err
	}

	// Extensions are optional in older TLS ClientHello messages.
	if offset == len(body) {
		return "", prefetched, errors.New("TLS ClientHello has no extensions or SNI")
	}

	field, err = readBytes(2)
	if err != nil {
		return "", prefetched, err
	}
	extensionsLen := int(binary.BigEndian.Uint16(field))
	extensions, err := readBytes(extensionsLen)
	if err != nil {
		return "", prefetched, err
	}
	if offset != len(body) {
		return "", prefetched, errors.New("malformed TLS ClientHello: trailing bytes")
	}

	for pos := 0; pos < len(extensions); {
		if pos+4 > len(extensions) {
			return "", prefetched, errors.New("malformed TLS extension header")
		}
		extType := binary.BigEndian.Uint16(extensions[pos : pos+2])
		extLen := int(binary.BigEndian.Uint16(extensions[pos+2 : pos+4]))
		pos += 4
		if pos+extLen > len(extensions) {
			return "", prefetched, errors.New("malformed TLS extension length")
		}
		extData := extensions[pos : pos+extLen]
		pos += extLen

		if extType != 0x0000 { // server_name
			continue
		}
		if len(extData) < 2 {
			return "", prefetched, errors.New("malformed SNI extension")
		}
		listLen := int(binary.BigEndian.Uint16(extData[:2]))
		if listLen != len(extData)-2 {
			return "", prefetched, errors.New("malformed SNI server name list")
		}

		for namePos := 2; namePos < len(extData); {
			if namePos+3 > len(extData) {
				return "", prefetched, errors.New("malformed SNI name entry")
			}
			nameType := extData[namePos]
			nameLen := int(binary.BigEndian.Uint16(extData[namePos+1 : namePos+3]))
			namePos += 3
			if namePos+nameLen > len(extData) {
				return "", prefetched, errors.New("malformed SNI name length")
			}
			name := string(extData[namePos : namePos+nameLen])
			namePos += nameLen

			if nameType == 0 {
				if !validSNIHostname(name) {
					return "", prefetched, fmt.Errorf("invalid SNI hostname %q", name)
				}
				return name, prefetched, nil
			}
		}
	}

	return "", prefetched, errors.New("SNI extension not found")
}

func validSNIHostname(host string) bool {
	if host == "" || len(host) > 253 || strings.TrimSpace(host) != host {
		return false
	}
	for i := 0; i < len(host); i++ {
		c := host[i]
		if c <= 0x20 || c >= 0x7f || c == '/' || c == '\\' || c == ':' {
			return false
		}
	}
	return true
}

// ============================================================
// SOCKS5 Proxy Dialer
// ============================================================

type SOCKS5Dialer struct {
	Address string
}

func (d *SOCKS5Dialer) Connect(
	target string,
) (net.Conn, io.Reader, error) {
	start := time.Now()

	conn, err := net.DialTimeout(
		"tcp",
		d.Address,
		connectTimeout,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("SOCKS5 dial: %w", err)
	}

	fail := func(err error) (net.Conn, io.Reader, error) {
		_ = conn.Close()
		return nil, nil, err
	}

	if err := conn.SetDeadline(
		time.Now().Add(connectTimeout),
	); err != nil {
		return fail(err)
	}

	if err := socks5Handshake(conn); err != nil {
		return fail(err)
	}

	if err := socks5Connect(conn, target); err != nil {
		return fail(err)
	}

	if err := conn.SetDeadline(time.Time{}); err != nil {
		return fail(err)
	}

	log.Printf(
		"[SOCKS5] CONNECT proxy=%s target=%s elapsed=%s",
		d.Address,
		target,
		time.Since(start),
	)

	return conn, conn, nil
}

func socks5Handshake(conn net.Conn) error {
	greeting := []byte{
		socksVersion,
		0x01,
		authNoAuth,
	}

	if err := writeFull(conn, greeting); err != nil {
		return fmt.Errorf("send greeting: %w", err)
	}

	response := make([]byte, 2)

	if _, err := io.ReadFull(conn, response); err != nil {
		return fmt.Errorf("read greeting: %w", err)
	}

	if response[0] != socksVersion {
		return fmt.Errorf("invalid SOCKS5 version: %d", response[0])
	}

	if response[1] != authNoAuth {
		return fmt.Errorf(
			"SOCKS5 no-auth rejected: method=0x%02x",
			response[1],
		)
	}

	return nil
}

func socks5Connect(conn net.Conn, target string) error {
	host, port, err := net.SplitHostPort(target)
	if err != nil {
		return err
	}

	portNum, err := strconv.Atoi(port)
	if err != nil || portNum < 1 || portNum > 65535 {
		return fmt.Errorf("invalid target port: %s", port)
	}

	request := []byte{
		socksVersion,
		cmdConnect,
		0x00,
	}

	ip := net.ParseIP(host)

	switch {
	case ip != nil && ip.To4() != nil:
		request = append(request, atypIPv4)
		request = append(request, ip.To4()...)

	case ip != nil:
		request = append(request, atypIPv6)
		request = append(request, ip.To16()...)

	default:
		domain := []byte(host)

		if len(domain) == 0 || len(domain) > 255 {
			return fmt.Errorf("invalid domain: %s", host)
		}

		request = append(request, atypDomain)
		request = append(request, byte(len(domain)))
		request = append(request, domain...)
	}

	var portBytes [2]byte
	binary.BigEndian.PutUint16(
		portBytes[:],
		uint16(portNum),
	)

	request = append(request, portBytes[:]...)

	if err := writeFull(conn, request); err != nil {
		return fmt.Errorf("send CONNECT: %w", err)
	}

	header := make([]byte, 4)

	if _, err := io.ReadFull(conn, header); err != nil {
		return fmt.Errorf("read CONNECT response: %w", err)
	}

	if header[0] != socksVersion {
		return errors.New("invalid SOCKS5 response version")
	}

	if header[1] != replySuccess {
		return fmt.Errorf(
			"SOCKS5 CONNECT rejected: REP=0x%02x",
			header[1],
		)
	}

	switch header[3] {
	case atypIPv4:
		_, err = io.CopyN(io.Discard, conn, 6)

	case atypIPv6:
		_, err = io.CopyN(io.Discard, conn, 18)

	case atypDomain:
		var length [1]byte

		if _, err = io.ReadFull(conn, length[:]); err == nil {
			_, err = io.CopyN(
				io.Discard,
				conn,
				int64(length[0])+2,
			)
		}

	default:
		return fmt.Errorf(
			"invalid SOCKS5 address type: %d",
			header[3],
		)
	}

	if err != nil {
		return fmt.Errorf("read SOCKS5 bound address: %w", err)
	}

	return nil
}

// ============================================================
// HTTP Proxy Dialer
// ============================================================

type HTTPProxyDialer struct {
	Address  string
	Username string
	Password string
}

func (d *HTTPProxyDialer) Connect(
	target string,
) (net.Conn, io.Reader, error) {
	start := time.Now()

	conn, err := net.DialTimeout(
		"tcp",
		d.Address,
		connectTimeout,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("HTTP proxy dial: %w", err)
	}

	fail := func(err error) (net.Conn, io.Reader, error) {
		_ = conn.Close()
		return nil, nil, err
	}

	if err := conn.SetDeadline(
		time.Now().Add(connectTimeout),
	); err != nil {
		return fail(err)
	}

	// Build HTTP CONNECT request.
	var request strings.Builder

	request.WriteString("CONNECT ")
	request.WriteString(target)
	request.WriteString(" HTTP/1.1\r\n")

	request.WriteString("Host: ")
	request.WriteString(target)
	request.WriteString("\r\n")

	request.WriteString("Proxy-Connection: Keep-Alive\r\n")

	if d.Username != "" {
		auth := base64.StdEncoding.EncodeToString(
			[]byte(d.Username + ":" + d.Password),
		)

		request.WriteString("Proxy-Authorization: Basic ")
		request.WriteString(auth)
		request.WriteString("\r\n")
	}

	request.WriteString("\r\n")

	if err := writeFull(conn, []byte(request.String())); err != nil {
		return fail(fmt.Errorf("send HTTP CONNECT: %w", err))
	}

	// Keep the buffered reader for forwarding.
	// It may already contain TLS bytes from the target server.
	reader := bufio.NewReader(conn)

	httpRequest := &http.Request{
		Method: http.MethodConnect,
		URL:    nil,
		Host:   target,
		Header: make(http.Header),
	}

	response, err := http.ReadResponse(reader, httpRequest)
	if err != nil {
		return fail(fmt.Errorf("read HTTP proxy response: %w", err))
	}

	if response.StatusCode != http.StatusOK {
		status := response.Status
		_ = response.Body.Close()

		return fail(fmt.Errorf(
			"HTTP proxy CONNECT rejected: %s",
			status,
		))
	}

	if err := conn.SetDeadline(time.Time{}); err != nil {
		return fail(err)
	}

	log.Printf(
		"[HTTP-PROXY] CONNECT proxy=%s target=%s status=%s elapsed=%s",
		d.Address,
		target,
		response.Status,
		time.Since(start),
	)

	return conn, reader, nil
}

// ============================================================
// Bidirectional TCP Forwarding
// ============================================================

type countWriter struct {
	w io.Writer
	n int64
}

func (c *countWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

func forward(
	id uint64,
	client net.Conn,
	remote net.Conn,
	clientReader io.Reader,
	remoteReader io.Reader,
	target string,
	connectionStart time.Time,
	established time.Time,
) {
	var wg sync.WaitGroup

	var stats TrafficStats

	wg.Add(2)

	// Client -> Server.
	go func() {
		defer wg.Done()

		start := time.Now()

		counter := &countWriter{
			w: remote,
		}

		n, err := io.Copy(counter, clientReader)

		end := time.Now()

		atomic.StoreInt64(
			&stats.ClientToServer,
			counter.n,
		)

		if err != nil {
			log.Printf(
				"[TRAFFIC][%d] CLIENT->SERVER error=%v",
				id,
				err,
			)
		}

		if tcp, ok := remote.(*net.TCPConn); ok {
			_ = tcp.CloseWrite()
		}

		log.Printf(
			"[TRAFFIC][%d] CLIENT->SERVER bytes=%d elapsed=%s start=%s end=%s",
			id,
			n,
			end.Sub(start),
			start.Format(time.RFC3339Nano),
			end.Format(time.RFC3339Nano),
		)
	}()

	// Server -> Client.
	go func() {
		defer wg.Done()

		start := time.Now()

		counter := &countWriter{
			w: client,
		}

		n, err := io.Copy(counter, remoteReader)

		end := time.Now()

		atomic.StoreInt64(
			&stats.ServerToClient,
			counter.n,
		)

		if err != nil {
			log.Printf(
				"[TRAFFIC][%d] SERVER->CLIENT error=%v",
				id,
				err,
			)
		}

		if tcp, ok := client.(*net.TCPConn); ok {
			_ = tcp.CloseWrite()
		}

		log.Printf(
			"[TRAFFIC][%d] SERVER->CLIENT bytes=%d elapsed=%s start=%s end=%s",
			id,
			n,
			end.Sub(start),
			start.Format(time.RFC3339Nano),
			end.Format(time.RFC3339Nano),
		)
	}()

	wg.Wait()

	end := time.Now()

	clientBytes := atomic.LoadInt64(
		&stats.ClientToServer,
	)

	serverBytes := atomic.LoadInt64(
		&stats.ServerToClient,
	)

	log.Printf(
		"[CONN][%d] CLOSED client=%s target=%s established=%s end=%s total_time=%s active_time=%s",
		id,
		client.RemoteAddr(),
		target,
		established.Format(time.RFC3339Nano),
		end.Format(time.RFC3339Nano),
		end.Sub(connectionStart),
		end.Sub(established),
	)

	log.Printf(
		"[TRAFFIC][%d] SUMMARY client_to_server=%d bytes server_to_client=%d bytes total=%d bytes",
		id,
		clientBytes,
		serverBytes,
		clientBytes+serverBytes,
	)
}

// ============================================================
// Common Utilities
// ============================================================

func writeFull(w io.Writer, data []byte) error {
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
