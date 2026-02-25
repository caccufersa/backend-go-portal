package services

import (
	"crypto/tls"
	"fmt"
	"net/smtp"
	"os"
	"strings"
)

// EmailService sends transactional e-mails via SMTP.
// Works with Gmail (SMTP_HOST=smtp.gmail.com / SMTP_PORT=587) using an App Password.
type EmailService interface {
	SendPasswordReset(toEmail, username, resetURL string) error
}

type emailService struct {
	host     string
	port     string
	username string
	password string
	from     string
	appName  string
}

func NewEmailService() EmailService {
	host := os.Getenv("SMTP_HOST")
	if host == "" {
		host = "smtp.gmail.com"
	}
	port := os.Getenv("SMTP_PORT")
	if port == "" {
		port = "587"
	}
	user := os.Getenv("SMTP_USERNAME")
	pass := os.Getenv("SMTP_PASSWORD")
	from := os.Getenv("SMTP_FROM")
	if from == "" {
		from = user
	}
	appName := os.Getenv("APP_NAME")
	if appName == "" {
		appName = "CACC Portal"
	}

	return &emailService{
		host:     host,
		port:     port,
		username: user,
		password: pass,
		from:     from,
		appName:  appName,
	}
}

func (e *emailService) SendPasswordReset(toEmail, username, resetURL string) error {
	if e.username == "" || e.password == "" {
		return fmt.Errorf("SMTP não configurado")
	}

	subject := fmt.Sprintf("Redefinição de senha – %s", e.appName)
	body := e.buildResetEmail(username, resetURL)

	return e.send(toEmail, subject, body)
}

func (e *emailService) send(to, subject, htmlBody string) error {
	addr := e.host + ":" + e.port

	headers := strings.Join([]string{
		"From: " + e.appName + " <" + e.from + ">",
		"To: " + to,
		"Subject: " + subject,
		"MIME-Version: 1.0",
		"Content-Type: text/html; charset=UTF-8",
	}, "\r\n")

	msg := []byte(headers + "\r\n\r\n" + htmlBody)

	auth := smtp.PlainAuth("", e.username, e.password, e.host)

	// Use STARTTLS (port 587) – standard for Gmail App Passwords
	tlsCfg := &tls.Config{
		InsecureSkipVerify: false,
		ServerName:         e.host,
	}

	conn, err := smtp.Dial(addr)
	if err != nil {
		return fmt.Errorf("falha ao conectar ao SMTP: %w", err)
	}
	defer conn.Close()

	if ok, _ := conn.Extension("STARTTLS"); ok {
		if err := conn.StartTLS(tlsCfg); err != nil {
			return fmt.Errorf("STARTTLS falhou: %w", err)
		}
	}

	if err := conn.Auth(auth); err != nil {
		return fmt.Errorf("autenticação SMTP falhou: %w", err)
	}

	if err := conn.Mail(e.from); err != nil {
		return err
	}
	if err := conn.Rcpt(to); err != nil {
		return err
	}

	wc, err := conn.Data()
	if err != nil {
		return err
	}
	defer wc.Close()

	_, err = wc.Write(msg)
	return err
}

func (e *emailService) buildResetEmail(username, resetURL string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="pt-BR">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>Redefinição de Senha</title>
  <style>
    body { margin:0; padding:0; background:#0f172a; font-family:'Segoe UI',system-ui,sans-serif; }
    .wrapper { max-width:520px; margin:40px auto; padding:0 16px; }
    .card {
      background: linear-gradient(135deg,#1e293b 0%%,#0f172a 100%%);
      border:1px solid #334155;
      border-radius:16px;
      overflow:hidden;
    }
    .header {
      background: linear-gradient(135deg,#6366f1 0%%,#8b5cf6 100%%);
      padding:32px 40px;
      text-align:center;
    }
    .header h1 { margin:0; color:#fff; font-size:22px; font-weight:700; letter-spacing:-0.5px; }
    .header p  { margin:6px 0 0; color:rgba(255,255,255,0.8); font-size:14px; }
    .body { padding:36px 40px; }
    .greeting { color:#e2e8f0; font-size:16px; margin:0 0 16px; }
    .message  { color:#94a3b8; font-size:14px; line-height:1.7; margin:0 0 28px; }
    .btn {
      display:block;
      width:fit-content;
      margin:0 auto 28px;
      padding:14px 36px;
      background: linear-gradient(135deg,#6366f1,#8b5cf6);
      color:#fff !important;
      text-decoration:none;
      border-radius:10px;
      font-size:15px;
      font-weight:600;
      letter-spacing:0.3px;
    }
    .divider { border:none; border-top:1px solid #1e293b; margin:0 0 20px; }
    .footer-text { color:#475569; font-size:12px; line-height:1.6; margin:0; }
    .link { color:#6366f1; word-break:break-all; }
    .expire { color:#f59e0b; font-weight:600; }
  </style>
</head>
<body>
  <div class="wrapper">
    <div class="card">
      <div class="header">
        <h1>🔐 Redefinição de Senha</h1>
        <p>%s</p>
      </div>
      <div class="body">
        <p class="greeting">Olá, <strong>%s</strong>!</p>
        <p class="message">
          Recebemos uma solicitação para redefinir a senha da sua conta.<br>
          Clique no botão abaixo para escolher uma nova senha.<br><br>
          <strong class="expire">⏱ Este link expira em 15 minutos.</strong>
        </p>
        <a href="%s" class="btn">Redefinir Minha Senha</a>
        <hr class="divider">
        <p class="footer-text">
          Se você não solicitou a redefinição de senha, ignore este e-mail — sua conta permanece segura.<br><br>
          Ou copie e cole este link no seu navegador:<br>
          <a href="%s" class="link">%s</a>
        </p>
      </div>
    </div>
  </div>
</body>
</html>`, e.appName, username, resetURL, resetURL, resetURL)
}
