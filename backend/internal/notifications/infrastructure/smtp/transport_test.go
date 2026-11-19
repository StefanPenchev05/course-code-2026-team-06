package smtp

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"mime"
	"net"
	"net/mail"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	email "github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/notifications/application"
)

func validConfig() Config {
	return Config{Host: "localhost", Port: 587, From: "tutti@uni.example", TLSMode: "starttls",
		ConnectTimeout: time.Second, SendTimeout: 2 * time.Second}
}

func TestConfiguration(t *testing.T) {
	for name, change := range map[string]func(*Config){
		"missing host":          func(c *Config) { c.Host = "" },
		"URL host":              func(c *Config) { c.Host = "smtp://localhost" },
		"empty label":           func(c *Config) { c.Host = "smtp..example" },
		"invalid label":         func(c *Config) { c.Host = "smtp.-example" },
		"long label":            func(c *Config) { c.Host = strings.Repeat("a", 64) + ".example" },
		"zero port":             func(c *Config) { c.Port = 0 },
		"large port":            func(c *Config) { c.Port = 65536 },
		"missing sender":        func(c *Config) { c.From = "" },
		"sender injection":      func(c *Config) { c.From = "sender@uni.example\r\nBcc: other@uni.example" },
		"sender display name":   func(c *Config) { c.From = "Tutti <sender@uni.example>" },
		"sender domain":         func(c *Config) { c.From = "sender@uni..example" },
		"username alone":        func(c *Config) { c.Username = "fake-user" },
		"password alone":        func(c *Config) { c.Password = "fake-secret" },
		"unknown TLS":           func(c *Config) { c.TLSMode = "opportunistic" },
		"remote plaintext":      func(c *Config) { c.TLSMode, c.Host = "plain", "smtp.example" },
		"plaintext credentials": func(c *Config) { c.TLSMode, c.Username, c.Password = "plain", "fake-user", "fake-secret" },
		"plaintext CA":          func(c *Config) { c.TLSMode, c.CAFile = "plain", "ca.pem" },
		"zero connect timeout":  func(c *Config) { c.ConnectTimeout = 0 },
		"negative send timeout": func(c *Config) { c.SendTimeout = -time.Second },
		"connect exceeds send":  func(c *Config) { c.ConnectTimeout = 3 * time.Second },
		"unbounded send":        func(c *Config) { c.SendTimeout = time.Minute + time.Second },
		"missing CA":            func(c *Config) { c.CAFile = filepath.Join(t.TempDir(), "absent.pem") },
	} {
		t.Run(name, func(t *testing.T) {
			c := validConfig()
			change(&c)
			if _, err := New(c); err == nil || strings.Contains(err.Error(), "fake-secret") {
				t.Fatalf("expected safe configuration error, got %v", err)
			}
		})
	}
	for _, mode := range []string{"starttls", "implicit", "plain"} {
		c := validConfig()
		c.TLSMode = mode
		if _, err := New(c); err != nil {
			t.Fatal(err)
		}
	}
	c := validConfig()
	c.CAFile = filepath.Join(t.TempDir(), "invalid.pem")
	if err := os.WriteFile(c.CAFile, []byte("not a certificate"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(c); err == nil {
		t.Fatal("invalid CA accepted")
	}
}

func testCertificate(t *testing.T, wrongHost, expired bool) (tls.Certificate, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Tutti test CA"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true,
		BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), NotBefore: time.Now().Add(-time.Hour),
		NotAfter: time.Now().Add(time.Hour), DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	if wrongHost {
		leaf.DNSNames, leaf.IPAddresses = []string{"wrong.example"}, nil
	}
	if expired {
		leaf.NotAfter = time.Now().Add(-time.Minute)
	}
	der, err := x509.CreateCertificate(rand.Reader, leaf, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), 0600); err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der, caDER}, PrivateKey: key}, path
}

type smtpScenario struct {
	mode                   string
	certificate            tls.Certificate
	noSTARTTLS, rejectAuth bool
	failAt, failure        string
	stall                  bool
}

// This server only listens on loopback and never relays a message.
func protocolServer(t *testing.T, scenario smtpScenario) (int, <-chan string, <-chan string, <-chan struct{}) {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	commands, messages := make(chan string, 32), make(chan string, 1)
	connected, done := make(chan struct{}), make(chan struct{})
	stop := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		close(connected)
		if scenario.stall {
			<-stop
			return
		}
		tlsConfig := &tls.Config{Certificates: []tls.Certificate{scenario.certificate}, MinVersion: tls.VersionTLS12}
		secure := scenario.mode == "implicit"
		if secure {
			conn = tls.Server(conn, tlsConfig)
		}
		reader := bufio.NewReader(conn)
		reply := func(s string) { _, _ = io.WriteString(conn, s+"\r\n") }
		reply("220 test.example ESMTP")
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimSpace(line)
			commands <- line
			if scenario.failAt != "" && strings.HasPrefix(line, scenario.failAt) {
				if scenario.failure != "" {
					reply(scenario.failure)
				}
				return
			}
			switch {
			case strings.HasPrefix(line, "EHLO"):
				reply("250-test.example")
				if !secure && scenario.mode == "starttls" && !scenario.noSTARTTLS {
					reply("250-STARTTLS")
				}
				if secure {
					reply("250-AUTH PLAIN")
				}
				reply("250 OK")
			case line == "STARTTLS":
				reply("220 Ready")
				conn = tls.Server(conn, tlsConfig)
				reader = bufio.NewReader(conn)
				secure = true
			case strings.HasPrefix(line, "AUTH PLAIN "):
				want := base64.StdEncoding.EncodeToString([]byte("\x00fake-user\x00fake-secret"))
				if !secure || scenario.rejectAuth || line != "AUTH PLAIN "+want {
					reply("535 rejected fake-secret")
				} else {
					reply("235 authenticated")
				}
			case strings.HasPrefix(line, "MAIL FROM:"), strings.HasPrefix(line, "RCPT TO:"):
				reply("250 OK")
			case line == "DATA":
				reply("354 data")
				body, err := textproto.NewReader(reader).ReadDotBytes()
				if err != nil {
					return
				}
				messages <- string(body)
				if scenario.failAt == "after-data" {
					if scenario.failure != "" {
						reply(scenario.failure)
					}
					return
				}
				reply("250 accepted")
				return // A disconnect after final acceptance must not cause retry.
			case line == "QUIT":
				reply("221 goodbye")
				return
			default:
				reply("500 unknown")
			}
		}
	}()
	t.Cleanup(func() { close(stop); _ = listener.Close(); <-done })
	return listener.Addr().(*net.TCPAddr).Port, commands, messages, connected
}

func TestSMTPProtocol(t *testing.T) {
	for _, tc := range []struct {
		name, mode, failAt, failure                      string
		noTLS, rejectAuth, untrusted, wrongHost, expired bool
		want                                             error
	}{
		{name: "STARTTLS authenticated", mode: "starttls"},
		{name: "implicit authenticated", mode: "implicit"},
		{name: "no downgrade", mode: "starttls", noTLS: true, want: email.TLSFailure},
		{name: "untrusted STARTTLS", mode: "starttls", untrusted: true, want: email.TLSFailure},
		{name: "untrusted implicit", mode: "implicit", untrusted: true, want: email.TLSFailure},
		{name: "hostname STARTTLS", mode: "starttls", wrongHost: true, want: email.TLSFailure},
		{name: "hostname implicit", mode: "implicit", wrongHost: true, want: email.TLSFailure},
		{name: "expired certificate", mode: "starttls", expired: true, want: email.TLSFailure},
		{name: "authentication rejection", mode: "starttls", rejectAuth: true, want: email.AuthenticationFailure},
		{name: "temporary recipient", mode: "starttls", failAt: "RCPT", failure: "450 4.2.0 private@example", want: email.TemporaryRejection},
		{name: "permanent recipient", mode: "starttls", failAt: "RCPT", failure: "550 5.1.1 private@example", want: email.PermanentRejection},
		{name: "temporary after DATA", mode: "starttls", failAt: "after-data", failure: "451 4.3.0 try again", want: email.TemporaryRejection},
		{name: "permanent after DATA", mode: "starttls", failAt: "after-data", failure: "554 5.7.1 rejected", want: email.PermanentRejection},
		{name: "disconnect", mode: "implicit", failAt: "RCPT", want: email.TransportFailure},
		{name: "ambiguous DATA disconnect", mode: "starttls", failAt: "after-data", want: email.TransportFailure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cert, ca := testCertificate(t, tc.wrongHost, tc.expired)
			port, commands, messages, _ := protocolServer(t, smtpScenario{mode: tc.mode, certificate: cert,
				noSTARTTLS: tc.noTLS, rejectAuth: tc.rejectAuth, failAt: tc.failAt, failure: tc.failure})
			c := validConfig()
			c.Host, c.Port, c.TLSMode, c.CAFile = "127.0.0.1", port, tc.mode, ca
			c.Username, c.Password = "fake-user", "fake-secret"
			if tc.untrusted {
				c.CAFile = ""
			}
			transport, err := New(c)
			if err != nil {
				t.Fatal(err)
			}
			err = transport.Send(context.Background(), email.Message{ID: "trace-id", Recipient: "student@uni.example", Subject: "Tutorat – accepté", Body: "Bonjour!\nÉtudiant"})
			if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
			if tc.want == nil {
				select {
				case m := <-messages:
					if !strings.Contains(m, "Message-ID: <trace-id@uni.example>") {
						t.Fatal("missing trace ID")
					}
				default:
					t.Fatal("message not accepted")
				}
			}
			if errors.Is(tc.want, email.TLSFailure) {
				for len(commands) > 0 {
					if command := <-commands; strings.HasPrefix(command, "AUTH") || strings.HasPrefix(command, "MAIL") {
						t.Fatal("credentials or message sent without verified TLS")
					}
				}
			}
		})
	}
}

func TestTimeoutAndCancellation(t *testing.T) {
	for _, cancelEarly := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelEarly), func(t *testing.T) {
			port, _, _, connected := protocolServer(t, smtpScenario{stall: true})
			c := validConfig()
			c.Host, c.Port, c.TLSMode = "127.0.0.1", port, "plain"
			c.ConnectTimeout, c.SendTimeout = 100*time.Millisecond, 100*time.Millisecond
			transport, err := New(c)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			result := make(chan error, 1)
			go func() {
				result <- transport.Send(ctx, email.Message{ID: "timeout", Recipient: "a@uni.example", Subject: "test"})
			}()
			<-connected
			want := error(email.TransportTimeout)
			if cancelEarly {
				cancel()
				want = context.Canceled
			}
			select {
			case err := <-result:
				if !errors.Is(err, want) {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("SMTP did not respect cancellation/timeout")
			}
		})
	}
}

func TestMessageFormattingAndInjection(t *testing.T) {
	transport, err := New(validConfig())
	if err != nil {
		t.Fatal(err)
	}
	m := email.Message{ID: "stable-id", Recipient: "student@uni.example", Subject: "Tutorat accepté – 日本語", Body: strings.Repeat("Élève\n", 40)}
	var out bytes.Buffer
	if err := transport.writeMessage(&out, m); err != nil {
		t.Fatal(err)
	}
	parsed, err := mail.ReadMessage(&out)
	if err != nil {
		t.Fatal(err)
	}
	subject, err := new(mime.WordDecoder).DecodeHeader(parsed.Header.Get("Subject"))
	if err != nil || subject != m.Subject {
		t.Fatal(subject, err)
	}
	body, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, parsed.Body))
	if err != nil || string(body) != m.Body {
		t.Fatal("body did not round trip", err)
	}
	if parsed.Header.Get("Message-ID") != "<stable-id@uni.example>" || parsed.Header.Get("From") != validConfig().From {
		t.Fatal("incorrect identity headers")
	}
	if _, err := mail.ParseDate(parsed.Header.Get("Date")); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*email.Message){
		func(m *email.Message) { m.Recipient = "a@uni.example\r\nBcc: b@uni.example" },
		func(m *email.Message) { m.Subject = "test\nBcc: b@uni.example" },
		func(m *email.Message) { m.Subject = "\t" },
		func(m *email.Message) { m.Subject = strings.Repeat("s", 241) },
		func(m *email.Message) { m.ID = "id>\r\nBcc: b@uni.example" },
	} {
		bad := m
		change(&bad)
		if err := transport.Send(context.Background(), bad); !errors.Is(err, email.InvalidMessage) {
			t.Fatalf("malformed headers accepted: %v", err)
		}
	}
}
