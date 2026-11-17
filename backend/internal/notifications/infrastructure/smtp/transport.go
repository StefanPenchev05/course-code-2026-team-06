// Package smtp sends notifications over a verified SMTP connection.
package smtp

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/StefanPenchev05/course-code-2026-team-06/backend/internal/notifications/application"
)

type Config struct {
	Host, From, Username, Password string
	Port                           int
	TLSMode                        string
	ConnectTimeout, SendTimeout    time.Duration
}

type Transport struct {
	config       Config
	senderDomain string
}

var hostname = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9.-]*[a-zA-Z0-9])?$`)
var messageID = regexp.MustCompile(`^[a-zA-Z0-9-]{1,100}$`)

func New(c Config) (*Transport, error) {
	if (net.ParseIP(c.Host) == nil && !hostname.MatchString(c.Host)) || c.Port < 1 || c.Port > 65535 {
		return nil, errors.New("configure SMTP_HOST and a valid SMTP_PORT")
	}
	domain := mailboxDomain(c.From)
	if domain == "" {
		return nil, errors.New("SMTP_FROM must be a plain email address")
	}
	if c.ConnectTimeout <= 0 || c.SendTimeout <= 0 || c.ConnectTimeout > c.SendTimeout {
		return nil, errors.New("SMTP_CONNECT_TIMEOUT must be positive and no longer than SMTP_SEND_TIMEOUT")
	}
	if (c.Username == "") != (c.Password == "") {
		return nil, errors.New("configure both SMTP_USERNAME and SMTP_PASSWORD, or leave both empty")
	}
	switch c.TLSMode {
	case "starttls", "implicit":
	case "plain":
		if (c.Host != "localhost" && !net.ParseIP(c.Host).IsLoopback()) || c.Username != "" {
			return nil, errors.New("plaintext SMTP is restricted to unauthenticated loopback testing")
		}
	default:
		return nil, errors.New("SMTP_TLS_MODE must be starttls, implicit, or plain")
	}
	return &Transport{config: c, senderDomain: domain}, nil
}

func mailboxDomain(value string) string {
	if len(value) > 254 || strings.ContainsAny(value, " \r\n<>") {
		return ""
	}
	for _, r := range value {
		if r < 33 || r > 126 {
			return ""
		}
	}
	parsed, err := mail.ParseAddress(value)
	if err != nil || parsed.Address != value {
		return ""
	}
	i := strings.LastIndexByte(value, '@')
	if i < 1 || !hostname.MatchString(value[i+1:]) {
		return ""
	}
	return value[i+1:]
}

func (t *Transport) Send(ctx context.Context, message application.Message) error {
	if mailboxDomain(message.Recipient) == "" || strings.ContainsAny(message.Subject, "\r\n\x00") ||
		strings.TrimSpace(message.Subject) == "" || len(message.Subject) > 240 ||
		!messageID.MatchString(message.ID) {
		return application.InvalidMessage
	}
	ctx, cancel := context.WithTimeout(ctx, t.config.SendTimeout)
	defer cancel()
	dialer := net.Dialer{Timeout: t.config.ConnectTimeout}
	raw, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(t.config.Host, strconv.Itoa(t.config.Port)))
	if err != nil {
		return classify(ctx, err)
	}
	defer raw.Close()
	stop := context.AfterFunc(ctx, func() { _ = raw.Close() })
	defer stop()
	if deadline, ok := ctx.Deadline(); ok {
		_ = raw.SetDeadline(deadline)
	}

	var conn net.Conn = raw
	tlsConfig := &tls.Config{ServerName: t.config.Host, MinVersion: tls.VersionTLS12}
	if t.config.TLSMode == "implicit" {
		secured := tls.Client(raw, tlsConfig)
		if err := secured.HandshakeContext(ctx); err != nil {
			if ctx.Err() != nil {
				return classify(ctx, err)
			}
			return application.TLSFailure
		}
		conn = secured
	}
	client, err := smtp.NewClient(conn, t.config.Host)
	if err != nil {
		return classify(ctx, err)
	}
	defer client.Close()
	if t.config.TLSMode == "starttls" {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return application.TLSFailure
		}
		if err := client.StartTLS(tlsConfig); err != nil {
			if ctx.Err() != nil {
				return classify(ctx, err)
			}
			return application.TLSFailure
		}
	}
	if t.config.Username != "" {
		auth := smtp.PlainAuth("", t.config.Username, t.config.Password, t.config.Host)
		if err := client.Auth(auth); err != nil {
			if ctx.Err() != nil {
				return classify(ctx, err)
			}
			return application.AuthenticationFailure
		}
	}
	if err := client.Mail(t.config.From); err != nil {
		return classify(ctx, err)
	}
	if err := client.Rcpt(message.Recipient); err != nil {
		return classify(ctx, err)
	}
	data, err := client.Data()
	if err != nil {
		return classify(ctx, err)
	}
	if err := t.writeMessage(data, message); err != nil {
		return classify(ctx, err)
	}
	// Closing DATA receives the server's final acceptance or refusal. A later
	// failure closing the connection must not turn acceptance into a retry.
	return classify(ctx, data.Close())
}

func (t *Transport) writeMessage(out io.Writer, m application.Message) error {
	_, err := fmt.Fprintf(out, "From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMessage-ID: <%s@%s>\r\n"+
		"MIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: base64\r\n\r\n",
		t.config.From, m.Recipient, mime.QEncoding.Encode("UTF-8", m.Subject), time.Now().UTC().Format(time.RFC1123Z), m.ID, t.senderDomain)
	if err != nil {
		return err
	}
	// 57 input bytes become 76 base64 characters per line.
	body := []byte(m.Body)
	for len(body) > 0 {
		n := min(len(body), 57)
		if _, err := io.WriteString(out, base64.StdEncoding.EncodeToString(body[:n])+"\r\n"); err != nil {
			return err
		}
		body = body[n:]
	}
	return nil
}

func classify(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return context.Canceled
	}
	var network net.Error
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || (errors.As(err, &network) && network.Timeout()) {
		return application.TransportTimeout
	}
	var reply *textproto.Error
	if errors.As(err, &reply) {
		if reply.Code >= 500 && reply.Code < 600 {
			return application.PermanentRejection
		}
		if reply.Code >= 400 && reply.Code < 500 {
			return application.TemporaryRejection
		}
	}
	return application.TransportFailure
}

// SendActivation satisfies Identity's trusted provisioning mail interface.
// The secret is sent directly through SMTP and never persisted in the outbox.
func (t *Transport) SendActivation(ctx context.Context, email, token string, ttl time.Duration) error {
	return t.Send(ctx, application.Message{ID: rand.Text(), Recipient: email,
		Subject: "Activate your Tutti coordinator account",
		Body:    fmt.Sprintf("Your Tutti coordinator activation token is:\n\n%s\n\nIt expires after %s.", token, ttl),
	})
}
