package client

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"

	"github.com/kakkky/gorviz/internal/app/server/domain"
)

// MailClient は利用者の SMTP サーバから送る
type MailClient struct{}

func NewMailClient() (*MailClient, error) { return &MailClient{}, nil }

func (c *MailClient) Send(ctx context.Context, dest domain.Destination, n domain.Notification) error {
	host, _, err := net.SplitHostPort(dest.SMTPAddr)
	if err != nil {
		return domain.ErrInternal.Wrap(err, "read the smtp address")
	}
	var auth smtp.Auth
	if dest.Username != "" {
		auth = smtp.PlainAuth("", dest.Username, dest.Password, host)
	}
	subject := fmt.Sprintf("[gorviz] %s %s (%s)", n.Kind, n.Subject, n.Service)
	body := fmt.Sprintf("サービス: %s\nインスタンス: %s\n種類: %s\n対象: %s\n確かさ: %s\n始まり: %s\n\n%s\n\n次に見ること: %s\n\n%s\n",
		n.Service, n.Instance, n.Kind, n.Subject, n.Certainty, n.StartedAt.UTC().Format(time.RFC3339), n.Reason, n.NextAction, n.URL)
	msg := "From: " + dest.From + "\r\nTo: " + strings.Join(dest.To, ", ") + "\r\nSubject: " + subject +
		"\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n" + body
	if err := sendMail(ctx, dest, host, auth, []byte(msg)); err != nil {
		return domain.ErrInternal.Wrap(err, "send a mail")
	}
	return nil
}

func sendMail(ctx context.Context, dest domain.Destination, host string, auth smtp.Auth, msg []byte) error {
	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", dest.SMTPAddr)
	if err != nil {
		return err
	}
	deadline, _ := ctx.Deadline()
	conn.SetDeadline(deadline)
	c, err := smtp.NewClient(conn, host)
	if err != nil {
		conn.Close()
		return err
	}
	defer c.Close()
	if ok, _ := c.Extension("STARTTLS"); ok {
		if err := c.StartTLS(&tls.Config{ServerName: host}); err != nil {
			return err
		}
	}
	if auth != nil {
		if err := c.Auth(auth); err != nil {
			return err
		}
	}
	if err := c.Mail(dest.From); err != nil {
		return err
	}
	for _, to := range dest.To {
		if err := c.Rcpt(to); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}
