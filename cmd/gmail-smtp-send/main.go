package main

import (
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/smtp"
	"mime/quotedprintable"
	"os"
	"strings"
	"time"
)

type sendMode string

const (
	modeAuto      sendMode = "auto"
	modeStartTLS  sendMode = "starttls"
	modeImplicit  sendMode = "tls"
	modePlain     sendMode = "plain"
	defaultServer          = "smtp.gmail.com"
)

func main() {
	var (
		host     = flag.String("host", defaultServer, "SMTP host")
		port     = flag.Int("port", 587, "SMTP port")
		mode     = flag.String("mode", string(modeAuto), "Transport mode: auto, starttls, tls, plain")
		from     = flag.String("from", "", "From address")
		to       = flag.String("to", "", "Recipient address")
		username = flag.String("username", "", "SMTP username (defaults to -from)")
		password = flag.String("password", "", "SMTP password/app password (or set GMAIL_APP_PASSWORD)")
		subject  = flag.String("subject", "Picoclaw Gmail SMTP test", "Mail subject")
		body     = flag.String("body", "Hello from the Gmail SMTP test app.", "Mail body")
		timeout  = flag.Duration("timeout", 15*time.Second, "Network timeout")
		verbose  = flag.Bool("v", false, "Verbose diagnostics")
	)
	flag.Parse()

	if *from == "" || *to == "" {
		fatalf("both -from and -to are required")
	}

	user := strings.TrimSpace(*username)
	if user == "" {
		user = strings.TrimSpace(*from)
	}
	pass := strings.TrimSpace(*password)
	if pass == "" {
		pass = strings.TrimSpace(os.Getenv("GMAIL_APP_PASSWORD"))
	}
	if pass == "" {
		pass = strings.TrimSpace(os.Getenv("SMTP_PASSWORD"))
	}
	if pass == "" {
		fatalf("missing password: set -password or GMAIL_APP_PASSWORD")
	}

	m := sendMode(strings.ToLower(strings.TrimSpace(*mode)))
	if m == modeAuto {
		if *port == 465 {
			m = modeImplicit
		} else {
			m = modeStartTLS
		}
	}
	if m != modeStartTLS && m != modeImplicit && m != modePlain {
		fatalf("invalid -mode %q (want auto, starttls, tls, plain)", m)
	}

	msg := buildMessage(*from, *to, *subject, *body)
	if *verbose {
		fmt.Printf("mode=%s host=%s port=%d from=%s to=%s user=%s\n", m, *host, *port, *from, *to, user)
	}

	if err := sendSMTP(*host, *port, m, user, pass, *from, *to, msg, *timeout, *verbose); err != nil {
		fatalf("%v", err)
	}
	fmt.Println("ok: message sent")
}

func sendSMTP(host string, port int, mode sendMode, username, password, from, to string, msg []byte, timeout time.Duration, verbose bool) error {
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	switch mode {
	case modeImplicit:
		if verbose {
			fmt.Printf("dial tls %s...\n", addr)
		}
		conn, err := tls.DialWithDialer(&net.Dialer{Timeout: timeout}, "tcp", addr, &tls.Config{ServerName: host})
		if err != nil {
			return fmt.Errorf("SMTP TLS dial failed: %w", err)
		}
		defer conn.Close()
		return sendWithClient(conn, host, username, password, from, to, msg, verbose)

	case modeStartTLS:
		if verbose {
			fmt.Printf("dial plain %s...\n", addr)
		}
		conn, err := net.DialTimeout("tcp", addr, timeout)
		if err != nil {
			return fmt.Errorf("SMTP dial failed: %w", err)
		}
		defer conn.Close()
		cl, err := smtp.NewClient(conn, host)
		if err != nil {
			return fmt.Errorf("SMTP client creation failed: %w", err)
		}
		defer cl.Close()

		if verbose {
			fmt.Println("ehlo + starttls...")
		}
		if ok, _ := cl.Extension("STARTTLS"); !ok {
			return errors.New("SMTP server does not support STARTTLS")
		}
		if err := cl.StartTLS(&tls.Config{ServerName: host}); err != nil {
			return fmt.Errorf("SMTP STARTTLS failed: %w", err)
		}
		return finishSend(cl, host, username, password, from, to, msg, verbose)

	case modePlain:
		if verbose {
			fmt.Printf("dial plain %s...\n", addr)
		}
		conn, err := net.DialTimeout("tcp", addr, timeout)
		if err != nil {
			return fmt.Errorf("SMTP dial failed: %w", err)
		}
		defer conn.Close()
		cl, err := smtp.NewClient(conn, host)
		if err != nil {
			return fmt.Errorf("SMTP client creation failed: %w", err)
		}
		defer cl.Close()
		return finishSend(cl, host, username, password, from, to, msg, verbose)

	default:
		return fmt.Errorf("unknown mode %q", mode)
	}
}

func sendWithClient(conn net.Conn, host, username, password, from, to string, msg []byte, verbose bool) error {
	cl, err := smtp.NewClient(conn, host)
	if err != nil {
		return fmt.Errorf("SMTP client creation failed: %w", err)
	}
	defer cl.Close()
	return finishSend(cl, host, username, password, from, to, msg, verbose)
}

func finishSend(cl *smtp.Client, host, username, password, from, to string, msg []byte, verbose bool) error {
	if username != "" {
		if verbose {
			fmt.Println("auth...")
		}
		auth := smtp.PlainAuth("", username, password, host)
		if err := cl.Auth(auth); err != nil {
			return fmt.Errorf("SMTP auth failed: %w", err)
		}
	}
	if verbose {
		fmt.Println("mail from...")
	}
	if err := cl.Mail(from); err != nil {
		return fmt.Errorf("SMTP MAIL FROM failed: %w", err)
	}
	if verbose {
		fmt.Println("rcpt to...")
	}
	if err := cl.Rcpt(to); err != nil {
		return fmt.Errorf("SMTP RCPT TO failed: %w", err)
	}
	if verbose {
		fmt.Println("data...")
	}
	w, err := cl.Data()
	if err != nil {
		return fmt.Errorf("SMTP DATA failed: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		return fmt.Errorf("SMTP write failed: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("SMTP close failed: %w", err)
	}
	if verbose {
		fmt.Println("quit...")
	}
	return cl.Quit()
}

func buildMessage(from, to, subject, body string) []byte {
	var b strings.Builder
	b.WriteString("From: ")
	b.WriteString(from)
	b.WriteString("\r\n")
	b.WriteString("To: ")
	b.WriteString(to)
	b.WriteString("\r\n")
	b.WriteString("Subject: ")
	b.WriteString(subject)
	b.WriteString("\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n")
	b.WriteString("Content-Transfer-Encoding: quoted-printable\r\n")
	b.WriteString("\r\n")
	qp := quotedprintable.NewWriter(&b)
	_, _ = qp.Write([]byte(body))
	_ = qp.Close()
	b.WriteString("\r\n")
	return []byte(b.String())
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}
