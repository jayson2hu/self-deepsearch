package identity

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/smtp"
	"net/url"
	"strings"
)

type SMTPSender struct {
	address        string
	host           string
	username       string
	password       string
	from           string
	implicitTLS    bool
	allowPlaintext bool
}

func (sender *SMTPSender) AllowsPlaintext() bool {
	return sender.allowPlaintext
}

func NewSMTPSender(rawURL string) (*SMTPSender, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse SMTP_URL: %w", err)
	}
	if parsed.Scheme != "smtp" && parsed.Scheme != "smtps" {
		return nil, errors.New("SMTP_URL scheme must be smtp or smtps")
	}
	host := parsed.Hostname()
	if host == "" {
		return nil, errors.New("SMTP_URL host is required")
	}
	port := parsed.Port()
	if port == "" {
		if parsed.Scheme == "smtps" {
			port = "465"
		} else {
			port = "587"
		}
	}
	username := ""
	password := ""
	if parsed.User != nil {
		username = parsed.User.Username()
		password, _ = parsed.User.Password()
	}
	from := strings.TrimSpace(parsed.Query().Get("from"))
	if _, err := NormalizeEmail(from); err != nil {
		return nil, errors.New("SMTP_URL requires a valid from query parameter")
	}
	return &SMTPSender{
		address: net.JoinHostPort(host, port), host: host,
		username: username, password: password, from: from, implicitTLS: parsed.Scheme == "smtps",
		allowPlaintext: parsed.Query().Get("insecure") == "true",
	}, nil
}

func (sender *SMTPSender) SendCode(ctx context.Context, recipient, purpose, code string) error {
	connection, err := (&net.Dialer{}).DialContext(ctx, "tcp", sender.address)
	if err != nil {
		return fmt.Errorf("connect SMTP: %w", err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		if err := connection.SetDeadline(deadline); err != nil {
			_ = connection.Close()
			return fmt.Errorf("set SMTP deadline: %w", err)
		}
	}
	if sender.implicitTLS {
		connection = tls.Client(connection, &tls.Config{ServerName: sender.host, MinVersion: tls.VersionTLS12})
	}
	client, err := smtp.NewClient(connection, sender.host)
	if err != nil {
		_ = connection.Close()
		return fmt.Errorf("create SMTP client: %w", err)
	}
	defer client.Close()
	if !sender.implicitTLS && !sender.allowPlaintext {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return errors.New("SMTP server does not support STARTTLS")
		}
		if err := client.StartTLS(&tls.Config{ServerName: sender.host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("start SMTP TLS: %w", err)
		}
	}
	if sender.username != "" {
		if err := client.Auth(smtp.PlainAuth("", sender.username, sender.password, sender.host)); err != nil {
			return fmt.Errorf("authenticate SMTP: %w", err)
		}
	}
	if err := client.Mail(sender.from); err != nil {
		return fmt.Errorf("set SMTP sender: %w", err)
	}
	if err := client.Rcpt(recipient); err != nil {
		return fmt.Errorf("set SMTP recipient: %w", err)
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("open SMTP body: %w", err)
	}
	subject, action, validity := challengeCopy(purpose)
	body := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s验证码：%s\r\n%s内有效，请勿转发。\r\n", sender.from, recipient, subject, action, code, validity)
	if _, err := io.WriteString(writer, body); err != nil {
		_ = writer.Close()
		return fmt.Errorf("write SMTP body: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("finish SMTP body: %w", err)
	}
	if err := client.Quit(); err != nil {
		return fmt.Errorf("finish SMTP session: %w", err)
	}
	return nil
}

func challengeCopy(purpose string) (string, string, string) {
	switch purpose {
	case PurposeSignup:
		return "幕鉴注册验证码", "注册", "10 分钟"
	case PurposePasswordReset:
		return "幕鉴密码重置验证码", "密码重置", "10 分钟"
	case PurposeInvitation:
		return "幕鉴后台邀请验证码", "接受后台邀请", "48 小时"
	default:
		return "幕鉴关闭账号验证码", "关闭账号", "10 分钟"
	}
}
