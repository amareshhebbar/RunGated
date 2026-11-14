package egress

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/amareshhebbar/rungated/internal/auth"
	"github.com/amareshhebbar/rungated/internal/core"
	"github.com/amareshhebbar/rungated/internal/gateway"
	"github.com/amareshhebbar/rungated/internal/jsonx"
)

type Config struct {
	InterceptTLS bool
	CADir        string
	MaxScanBytes int64
	ParentProxy  string
}

type Proxy struct {
	Engine    *gateway.Engine
	Resolver  *auth.Resolver
	Cfg       Config
	CA        *CA
	Transport *http.Transport
	dialer    *net.Dialer
}

func denyLinkLocal(network, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && (ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast()) {
		return errors.New("connections to link local addresses are refused")
	}
	return nil
}

func New(e *gateway.Engine, r *auth.Resolver, cfg Config) (*Proxy, error) {
	if cfg.MaxScanBytes <= 0 {
		cfg.MaxScanBytes = 2 << 20
	}
	p := &Proxy{Engine: e, Resolver: r, Cfg: cfg}
	p.dialer = &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second, Control: denyLinkLocal}
	p.Transport = &http.Transport{
		DialContext:           p.dialer.DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 2 * time.Minute,
		MaxIdleConnsPerHost:   32,
		IdleConnTimeout:       90 * time.Second,
		DisableCompression:    true,
	}
	if cfg.ParentProxy != "" {
		u, err := url.Parse(cfg.ParentProxy)
		if err != nil {
			return nil, err
		}
		p.Transport.Proxy = http.ProxyURL(u)
	}
	if cfg.InterceptTLS {
		ca, err := LoadOrCreateCA(cfg.CADir)
		if err != nil {
			return nil, err
		}
		p.CA = ca
	}
	return p, nil
}

var hopHeaders = []string{"Connection", "Proxy-Connection", "Keep-Alive", "Proxy-Authenticate", "Proxy-Authorization", "Te", "Trailer", "Transfer-Encoding", "Upgrade"}

func stripHop(h http.Header) {
	for _, c := range h.Values("Connection") {
		for _, f := range strings.Split(c, ",") {
			if f = strings.TrimSpace(f); f != "" {
				h.Del(f)
			}
		}
	}
	for _, k := range hopHeaders {
		h.Del(k)
	}
	for k := range h {
		if strings.HasPrefix(http.CanonicalHeaderKey(k), "X-Rungated-") {
			h.Del(k)
		}
	}
}

func jsonReply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(jsonx.Marshal(v))
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodConnect:
		p.connect(w, r)
	case r.URL.IsAbs():
		id, err := p.identify(w, r)
		if err != nil {
			return
		}
		if r.Header.Get("Upgrade") != "" {
			jsonReply(w, http.StatusNotImplemented, map[string]any{"error": "protocol upgrades are not supported through the egress proxy"})
			return
		}
		scheme := strings.ToLower(r.URL.Scheme)
		if scheme != "http" && scheme != "https" {
			jsonReply(w, http.StatusBadRequest, map[string]any{"error": "unsupported scheme " + scheme})
			return
		}
		p.handle(w, r, id, scheme, r.URL.Host)
	case r.URL.Path == "/v1/egress/ca.pem" && p.CA != nil:
		w.Header().Set("Content-Type", "application/x-pem-file")
		w.Write(p.CA.PEM())
	case r.URL.Path == "/health":
		jsonReply(w, 200, map[string]any{"status": "ok"})
	default:
		jsonReply(w, http.StatusBadRequest, map[string]any{"error": "this is a forward proxy: send absolute URI requests or CONNECT"})
	}
}

func (p *Proxy) identify(w http.ResponseWriter, r *http.Request) (*auth.Identity, error) {
	id, err := p.Resolver.ResolveProxyAuth(r.Header.Get("Proxy-Authorization"), r)
	if err != nil {
		w.Header().Set("Proxy-Authenticate", `Basic realm="rungated"`)
		jsonReply(w, http.StatusProxyAuthRequired, map[string]any{"error": err.Error()})
		return nil, err
	}
	return id, nil
}

func splitHost(hostport, defPort string) (host, full string) {
	h, port, err := net.SplitHostPort(hostport)
	if err != nil {
		h = strings.Trim(hostport, "[]")
		port = defPort
	}
	return strings.ToLower(h), net.JoinHostPort(h, port)
}

var metadataHosts = map[string]bool{
	"169.254.169.254": true, "metadata.google.internal": true, "metadata": true, "100.100.100.200": true, "fd00:ec2::254": true, "169.254.170.2": true,
}

func metadataFinding(host string) []core.Finding {
	if metadataHosts[host] {
		return []core.Finding{{Detector: "egress", Category: core.CatEgress, Rule: "cloud_metadata", Severity: 1, Confidence: 1, Message: "destination " + host + " is a cloud metadata endpoint", Min: core.VerdictBlock}}
	}
	return nil
}

func textual(ct string) bool {
	ct = strings.ToLower(ct)
	if ct == "" {
		return true
	}
	switch {
	case strings.HasPrefix(ct, "text/"), strings.Contains(ct, "json"), strings.Contains(ct, "xml"), strings.Contains(ct, "javascript"), strings.Contains(ct, "x-www-form-urlencoded"), strings.Contains(ct, "yaml"), strings.Contains(ct, "multipart/"), strings.Contains(ct, "graphql"):
		return true
	}
	return false
}

func looksText(b []byte) bool {
	n := len(b)
	if n > 4096 {
		n = 4096
	}
	return utf8.Valid(b[:n]) && !bytes.Contains(b[:n], []byte{0})
}

func (p *Proxy) deny(w http.ResponseWriter, out *gateway.Outcome, fallback string) {
	d := out.Decision
	msg := out.Message()
	if msg == "" {
		msg = fallback
	}
	setVerdict(w.Header(), out)
	jsonReply(w, http.StatusForbidden, map[string]any{"error": msg, "verdict": d.Verdict, "score": d.Score, "action_id": d.ActionID})
}

func setVerdict(h http.Header, out *gateway.Outcome) {
	h.Set("X-Rungated-Verdict", string(out.Decision.Verdict))
	h.Set("X-Rungated-Score", fmt.Sprint(out.Decision.Score))
	h.Set("X-Rungated-Action-Id", out.Decision.ActionID)
}

func (p *Proxy) decide(ctx context.Context, a *core.Action) *gateway.Outcome {
	out := p.Engine.Decide(ctx, a)
	if out.Decision.Verdict == core.VerdictHold {
		p.Engine.Await(ctx, out)
	}
	return out
}

func (p *Proxy) handle(w http.ResponseWriter, r *http.Request, id *auth.Identity, scheme, hostport string) {
	defPort := "80"
	if scheme == "https" {
		defPort = "443"
	}
	host, full := splitHost(hostport, defPort)
	ag := p.Engine.Registry.Get(id.AgentID)
	scan, capBytes := true, int64(1<<20)
	if ag != nil {
		eg := ag.Manifest.Spec.Egress
		if eg.ScanBodies != nil {
			scan = *eg.ScanBodies
		}
		if eg.MaxBodyKB > 0 {
			capBytes = int64(eg.MaxBodyKB) << 10
		}
	}
	target := &url.URL{Scheme: scheme, Host: hostport, Path: r.URL.Path, RawPath: r.URL.RawPath, RawQuery: r.URL.RawQuery}
	if (scheme == "http" && strings.HasSuffix(full, ":80")) || (scheme == "https" && strings.HasSuffix(full, ":443")) {
		target.Host = host
		if strings.Contains(host, ":") {
			target.Host = "[" + host + "]"
		}
	}

	var buffered []byte
	var rest io.Reader = http.NoBody
	if r.Body != nil && r.Body != http.NoBody {
		b, _ := io.ReadAll(io.LimitReader(r.Body, capBytes+1))
		buffered = b
		rest = io.MultiReader(bytes.NewReader(b), r.Body)
		if int64(len(b)) <= capBytes {
			rest = bytes.NewReader(b)
		}
	}
	scanned := buffered
	if int64(len(scanned)) > capBytes {
		scanned = scanned[:capBytes]
	}
	size := int(r.ContentLength)
	if size < 0 {
		size = len(buffered)
	}

	a := &core.Action{AgentID: id.AgentID, SessionID: id.SessionID, Channel: core.ChannelEgress, Kind: core.KindEgressRequest, Host: host, Method: strings.ToUpper(r.Method), URL: target.String(), Bytes: size, Meta: map[string]string{"scheme": scheme}}
	if r.URL.RawQuery != "" {
		a.Texts = append(a.Texts, core.TextPart{Source: core.SourceEgressBody, Text: r.URL.RawQuery, Path: "query"})
		if dec, err := url.QueryUnescape(r.URL.RawQuery); err == nil && dec != r.URL.RawQuery {
			a.Texts = append(a.Texts, core.TextPart{Source: core.SourceEgressBody, Text: dec, Path: "query.decoded"})
		}
	}
	if scan && len(scanned) > 0 && (textual(r.Header.Get("Content-Type")) || looksText(scanned)) && looksText(scanned) {
		txt := string(scanned)
		a.Texts = append(a.Texts, core.TextPart{Source: core.SourceEgressBody, Text: txt, Path: "body"})
		if strings.Contains(strings.ToLower(r.Header.Get("Content-Type")), "x-www-form-urlencoded") {
			if dec, err := url.QueryUnescape(txt); err == nil && dec != txt {
				a.Texts = append(a.Texts, core.TextPart{Source: core.SourceEgressBody, Text: dec, Path: "body.decoded"})
			}
		}
	}
	a.Precomputed = metadataFinding(host)
	out := p.decide(r.Context(), a)
	if !out.Allowed {
		p.deny(w, out, "Rungated blocked this outbound request")
		return
	}

	req := r.Clone(r.Context())
	req.URL = target
	req.Host = target.Host
	req.RequestURI = ""
	req.Body = io.NopCloser(rest)
	req.ContentLength = r.ContentLength
	stripHop(req.Header)
	req.Header.Set("Accept-Encoding", "identity")
	resp, err := p.Transport.RoundTrip(req)
	if err != nil {
		slog.Warn("egress upstream failed", "host", full, "err", err)
		setVerdict(w.Header(), out)
		jsonReply(w, http.StatusBadGateway, map[string]any{"error": "upstream unreachable: " + err.Error()})
		return
	}
	defer resp.Body.Close()
	p.respond(w, r, id, resp, host, target.String(), out)
}

func (p *Proxy) respond(w http.ResponseWriter, r *http.Request, id *auth.Identity, resp *http.Response, host, fullURL string, reqOut *gateway.Outcome) {
	h := w.Header()
	for k, vs := range resp.Header {
		h[k] = append([]string(nil), vs...)
	}
	stripHop(h)
	setVerdict(h, reqOut)
	ct := resp.Header.Get("Content-Type")
	enc := strings.ToLower(resp.Header.Get("Content-Encoding"))
	scannable := r.Method != http.MethodHead && resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotModified &&
		textual(ct) && (enc == "" || enc == "identity") && !strings.HasPrefix(strings.ToLower(ct), "text/event-stream") && ct != ""
	if !scannable {
		w.WriteHeader(resp.StatusCode)
		copyStream(w, resp.Body, strings.HasPrefix(strings.ToLower(ct), "text/event-stream"))
		return
	}
	buf, _ := io.ReadAll(io.LimitReader(resp.Body, p.Cfg.MaxScanBytes+1))
	over := int64(len(buf)) > p.Cfg.MaxScanBytes
	scan := buf
	if over {
		scan = buf[:p.Cfg.MaxScanBytes]
	}
	a := &core.Action{AgentID: id.AgentID, SessionID: id.SessionID, Channel: core.ChannelEgress, Kind: core.KindEgressResponse, Host: host, URL: fullURL, Method: strings.ToUpper(r.Method), Bytes: len(buf), Texts: []core.TextPart{{Source: core.SourceWeb, Text: string(scan), Path: "body"}}}
	out := p.decide(r.Context(), a)
	if !out.Allowed {
		p.deny(w, out, "Rungated blocked this response")
		return
	}
	if len(out.Texts) == 1 && out.Texts[0] != string(scan) {
		repl := []byte(out.Texts[0])
		h.Del("Content-Encoding")
		h.Del("Etag")
		h.Set("Content-Length", fmt.Sprint(len(repl)))
		h.Set("X-Rungated-Verdict", string(out.Decision.Verdict))
		w.WriteHeader(resp.StatusCode)
		w.Write(repl)
		return
	}
	h.Set("X-Rungated-Response-Verdict", string(out.Decision.Verdict))
	w.WriteHeader(resp.StatusCode)
	w.Write(buf)
	if over {
		copyStream(w, resp.Body, false)
	}
}

func copyStream(w http.ResponseWriter, src io.Reader, flush bool) {
	f, _ := w.(http.Flusher)
	buf := make([]byte, 32*1024)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			if flush && f != nil {
				f.Flush()
			}
		}
		if err != nil {
			return
		}
	}
}

func (p *Proxy) connect(w http.ResponseWriter, r *http.Request) {
	id, err := p.identify(w, r)
	if err != nil {
		return
	}
	host, full := splitHost(r.Host, "443")
	a := &core.Action{AgentID: id.AgentID, SessionID: id.SessionID, Channel: core.ChannelEgress, Kind: core.KindEgressRequest, Host: host, Method: "CONNECT", URL: "https://" + full, Meta: map[string]string{"tunnel": "1", "scheme": "https"}}
	a.Precomputed = metadataFinding(host)
	out := p.decide(r.Context(), a)
	if !out.Allowed {
		p.deny(w, out, "Rungated blocked this tunnel")
		return
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		jsonReply(w, http.StatusInternalServerError, map[string]any{"error": "hijacking not supported"})
		return
	}
	if p.CA != nil && p.Cfg.InterceptTLS {
		conn, brw, err := hj.Hijack()
		if err != nil {
			return
		}
		conn.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
		p.mitm(&bufConn{Conn: conn, r: brw.Reader}, id, host, full)
		return
	}
	up, err := p.dialer.DialContext(r.Context(), "tcp", full)
	if err != nil {
		setVerdict(w.Header(), out)
		jsonReply(w, http.StatusBadGateway, map[string]any{"error": "cannot reach " + full + ": " + err.Error()})
		return
	}
	conn, brw, err := hj.Hijack()
	if err != nil {
		up.Close()
		return
	}
	conn.Write([]byte("HTTP/1.1 200 Connection Established\r\nX-Rungated-Action-Id: " + out.Decision.ActionID + "\r\n\r\n"))
	if n := brw.Reader.Buffered(); n > 0 {
		b, _ := brw.Reader.Peek(n)
		up.Write(b)
	}
	var wg sync.WaitGroup
	wg.Add(2)
	pipe := func(dst, src net.Conn) {
		defer wg.Done()
		io.Copy(dst, src)
		if cw, ok := dst.(interface{ CloseWrite() error }); ok {
			cw.CloseWrite()
		} else {
			dst.Close()
		}
	}
	go pipe(up, conn)
	go pipe(conn, up)
	wg.Wait()
	up.Close()
	conn.Close()
}

type bufConn struct {
	net.Conn
	r *bufio.Reader
}

func (b *bufConn) Read(p []byte) (int, error) { return b.r.Read(p) }

type oneConn struct {
	ch   chan net.Conn
	done chan struct{}
	once sync.Once
	addr net.Addr
}

type notifyConn struct {
	net.Conn
	l *oneConn
}

func (c *notifyConn) Close() error {
	c.l.once.Do(func() { close(c.l.done) })
	return c.Conn.Close()
}

func (l *oneConn) Accept() (net.Conn, error) {
	select {
	case c := <-l.ch:
		return &notifyConn{Conn: c, l: l}, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *oneConn) Close() error {
	l.once.Do(func() { close(l.done) })
	return nil
}

func (l *oneConn) Addr() net.Addr { return l.addr }

func (p *Proxy) mitm(conn net.Conn, id *auth.Identity, host, full string) {
	tc := tls.Server(conn, &tls.Config{
		MinVersion: tls.VersionTLS12,
		NextProtos: []string{"http/1.1"},
		GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
			return p.CA.Leaf(host)
		},
	})
	hctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := tc.HandshakeContext(hctx); err != nil {
		conn.Close()
		return
	}
	l := &oneConn{ch: make(chan net.Conn, 1), done: make(chan struct{}), addr: conn.LocalAddr()}
	l.ch <- tc
	srv := &http.Server{
		ReadHeaderTimeout: 15 * time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if rh, _ := splitHost(r.Host, "443"); rh != host {
				jsonReply(w, http.StatusMisdirectedRequest, map[string]any{"error": "Host header does not match the tunnel destination"})
				return
			}
			if r.Header.Get("Upgrade") != "" {
				jsonReply(w, http.StatusNotImplemented, map[string]any{"error": "protocol upgrades are not supported through the egress proxy"})
				return
			}
			p.handle(w, r, id, "https", full)
		}),
	}
	srv.Serve(l)
}
