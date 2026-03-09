package services

import (
	"crypto/tls"
	"fmt"
	"net/smtp"
	"os"
	"strings"

	"github.com/resend/resend-go/v3"
)

// EmailService sends transactional e-mails.
// Supports two providers:
//   - "resend" (default) – uses the Resend HTTP API (works on Render, Heroku, etc.)
//   - "smtp"            – connects directly via SMTP/STARTTLS (works locally / on VPS)
//
// Set EMAIL_PROVIDER=smtp to use SMTP; any other value (or empty) uses Resend.
type EmailService interface {
	SendPasswordReset(toEmail, username, resetURL string) error
	SendEmailVerification(toEmail, username, verifyURL string) error
}

// ---------------------------------------------------------------------------
// Shared struct
// ---------------------------------------------------------------------------

type emailService struct {
	// provider is "resend" or "smtp"
	provider string

	// SMTP fields
	host     string
	port     string
	username string
	password string
	from     string
	appName  string

	// Resend fields
	resendAPIKey string
	resendFrom   string
}

func NewEmailService() EmailService {
	provider := strings.ToLower(os.Getenv("EMAIL_PROVIDER"))
	if provider == "" {
		provider = "resend"
	}

	appName := os.Getenv("APP_NAME")
	if appName == "" {
		appName = "CACC Portal"
	}

	svc := &emailService{
		provider: provider,
		appName:  appName,
	}

	switch provider {
	case "smtp":
		svc.host = envOrDefault("SMTP_HOST", "smtp.gmail.com")
		svc.port = envOrDefault("SMTP_PORT", "587")
		svc.username = os.Getenv("SMTP_USERNAME")
		svc.password = os.Getenv("SMTP_PASSWORD")
		svc.from = os.Getenv("SMTP_FROM")
		if svc.from == "" {
			svc.from = svc.username
		}
	default: // resend
		svc.resendAPIKey = os.Getenv("RESEND_API_KEY")
		svc.resendFrom = os.Getenv("RESEND_FROM")
		if svc.resendFrom == "" {
			svc.resendFrom = fmt.Sprintf("%s <noreply@capcom.page>", appName)
		}
	}

	return svc
}

// ---------------------------------------------------------------------------
// Public method
// ---------------------------------------------------------------------------

func (e *emailService) SendPasswordReset(toEmail, username, resetURL string) error {
	if e.provider == "smtp" {
		if e.username == "" || e.password == "" {
			return fmt.Errorf("SMTP não configurado (SMTP_USERNAME/SMTP_PASSWORD ausentes no ambiente)")
		}
	} else {
		if e.resendAPIKey == "" {
			return fmt.Errorf("Resend não configurado (RESEND_API_KEY ausente no ambiente)")
		}
	}

	subject := fmt.Sprintf("Redefinição de senha – %s", e.appName)
	body := e.buildResetEmail(username, resetURL)

	return e.send(toEmail, subject, body)
}

func (e *emailService) SendEmailVerification(toEmail, username, verifyURL string) error {
	if e.provider == "smtp" {
		if e.username == "" || e.password == "" {
			return fmt.Errorf("SMTP não configurado")
		}
	} else {
		if e.resendAPIKey == "" {
			return fmt.Errorf("Resend não configurado")
		}
	}

	subject := fmt.Sprintf("Verificação de E-mail – %s", e.appName)
	body := e.buildVerificationEmail(username, verifyURL)

	return e.send(toEmail, subject, body)
}

// ---------------------------------------------------------------------------
// Send dispatcher
// ---------------------------------------------------------------------------

func (e *emailService) send(to, subject, htmlBody string) error {
	if e.provider == "smtp" {
		return e.sendSMTP(to, subject, htmlBody)
	}
	return e.sendResend(to, subject, htmlBody)
}

// ---------------------------------------------------------------------------
// Resend (HTTP API) – works on Render / any cloud
// ---------------------------------------------------------------------------

func (e *emailService) sendResend(to, subject, htmlBody string) error {
	client := resend.NewClient(e.resendAPIKey)

	params := &resend.SendEmailRequest{
		From:    e.resendFrom,
		To:      []string{to},
		Subject: subject,
		Html:    htmlBody,
	}

	_, err := client.Emails.Send(params)
	if err != nil {
		return fmt.Errorf("falha ao conectar à API do Resend: %v", err)
	}

	return nil
}

// ---------------------------------------------------------------------------
// SMTP (direct) – works locally / on VPS
// ---------------------------------------------------------------------------

func (e *emailService) sendSMTP(to, subject, htmlBody string) error {
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

// ---------------------------------------------------------------------------
// HTML template
// ---------------------------------------------------------------------------

func (e *emailService) buildResetEmail(username, resetURL string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="pt-BR">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Redefinição de Senha - %[1]s</title>
    <style type="text/css">
        body, table, td, a { -webkit-text-size-adjust: 100%%; -ms-text-size-adjust: 100%%; }
        table, td { mso-table-lspace: 0pt; mso-table-rspace: 0pt; }
        img { -ms-interpolation-mode: bicubic; }
        img { border: 0; height: auto; line-height: 100%%; outline: none; text-decoration: none; }
        table { border-collapse: collapse !important; }
        body { height: 100%% !important; margin: 0 !important; padding: 0 !important; width: 100%% !important; }
        
        /* Efeito hover para clientes modernos */
        .win-button:hover {
            border-top: 2px solid #000000 !important;
            border-left: 2px solid #000000 !important;
            border-bottom: 2px solid #ffffff !important;
            border-right: 2px solid #ffffff !important;
            padding: 6px 14px 4px 16px !important; /* Simula o botão sendo pressionado */
        }
    </style>
</head>
<body style="margin: 0; padding: 0; background-color: #008080; font-family: 'MS Sans Serif', Tahoma, Geneva, sans-serif;">

    <table border="0" cellpadding="0" cellspacing="0" width="100%%" style="background-color: #008080; padding: 40px 20px;">
        <tr>
            <td align="center">
                
                <table border="0" cellpadding="2" cellspacing="0" width="100%%" style="max-width: 450px; background-color: #c0c0c0; border-top: 2px solid #ffffff; border-left: 2px solid #ffffff; border-bottom: 2px solid #000000; border-right: 2px solid #000000;">
                    <tr>
                        <td>
                            
                            <table border="0" cellpadding="4" cellspacing="0" width="100%%" style="background-color: #000080; border: 1px solid #c0c0c0;">
                                <tr>
                                    <td style="color: #ffffff; font-weight: bold; font-size: 13px; font-family: 'MS Sans Serif', Tahoma, sans-serif; letter-spacing: 0.5px;">
                                        Recuperação_de_Senha.exe
                                    </td>
                                    <td align="right" width="20">
                                        <table border="0" cellpadding="0" cellspacing="0" style="background-color: #c0c0c0; border-top: 1px solid #ffffff; border-left: 1px solid #ffffff; border-bottom: 1px solid #000000; border-right: 1px solid #000000; height: 16px; width: 16px;">
                                            <tr>
                                                <td align="center" valign="middle" style="color: #000000; font-size: 10px; font-weight: bold; font-family: Arial, sans-serif; line-height: 1;">
                                                    X
                                                </td>
                                            </tr>
                                        </table>
                                    </td>
                                </tr>
                            </table>

                            <table border="0" cellpadding="15" cellspacing="0" width="100%%">
                                <tr>
                                    <td style="color: #000000; font-size: 13px; font-family: 'MS Sans Serif', Tahoma, sans-serif; line-height: 1.5;">
                                        <p style="margin-top: 0;"><b>Aviso do Sistema para %[2]s:</b></p>
                                        <p>Uma operação ilegal... brincadeira! Recebemos uma solicitação para redefinir a senha da sua conta no <b>%[1]s</b>.</p>
                                        <p>Se você não fez essa solicitação, pode ignorar este aviso de segurança com tranquilidade e continuar navegando.</p>
                                        <p>Para executar a redefinição de senha, por favor clique no botão de comando abaixo (expira em 15 minutos):</p>

                                        <table border="0" cellpadding="0" cellspacing="0" width="100%%" style="margin: 25px 0;">
                                            <tr>
                                                <td align="center">
                                                    <table border="0" cellpadding="0" cellspacing="0" class="win-button" style="background-color: #c0c0c0; border-top: 2px solid #ffffff; border-left: 2px solid #ffffff; border-bottom: 2px solid #000000; border-right: 2px solid #000000;">
                                                        <tr>
                                                            <td align="center" style="padding: 5px 15px;">
                                                                <a href="%[3]s" target="_blank" style="text-decoration: none; color: #000000; font-weight: bold; font-size: 13px; font-family: 'MS Sans Serif', Tahoma, sans-serif; display: block;">
                                                                    &nbsp;CONFIRMAR REDEFINIÇÃO&nbsp;
                                                                </a>
                                                            </td>
                                                        </tr>
                                                    </table>
                                                </td>
                                            </tr>
                                        </table>

                                        <hr style="border: none; border-top: 1px solid #808080; border-bottom: 1px solid #ffffff; margin: 20px 0;">

                                        <p style="margin: 0; font-size: 11px; text-align: center; color: #555555;">
                                            Falha ao executar o comando? Copie e cole este caminho no seu navegador:<br>
                                            <a href="%[3]s" style="color: #000080; text-decoration: underline; word-break: break-all;">
                                               %[3]s
                                            </a>
                                        </p>
                                    </td>
                                </tr>
                            </table>

                        </td>
                    </tr>
                </table>
                <p style="color: #ffffff; font-size: 11px; font-family: 'MS Sans Serif', Tahoma, sans-serif; text-align: center; margin-top: 20px;">
                    © 2006-2026 %[1]s. Todos os direitos reservados.
                </p>

            </td>
        </tr>
    </table>

</body>
</html>`, e.appName, username, resetURL)
}

func (e *emailService) buildVerificationEmail(username, verifyURL string) string {
	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="pt-BR">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Verifique seu E-mail - %[1]s</title>
    <style type="text/css">
        body, table, td, a { -webkit-text-size-adjust: 100%%; -ms-text-size-adjust: 100%%; }
        table, td { mso-table-lspace: 0pt; mso-table-rspace: 0pt; }
        img { -ms-interpolation-mode: bicubic; }
        img { border: 0; height: auto; line-height: 100%%; outline: none; text-decoration: none; }
        table { border-collapse: collapse !important; }
        body { height: 100%% !important; margin: 0 !important; padding: 0 !important; width: 100%% !important; }
        
        .win-button:hover {
            border-top: 2px solid #000000 !important;
            border-left: 2px solid #000000 !important;
            border-bottom: 2px solid #ffffff !important;
            border-right: 2px solid #ffffff !important;
            padding: 6px 14px 4px 16px !important;
        }
    </style>
</head>
<body style="margin: 0; padding: 0; background-color: #008080; font-family: 'MS Sans Serif', Tahoma, Geneva, sans-serif;">

    <table border="0" cellpadding="0" cellspacing="0" width="100%%" style="background-color: #008080; padding: 40px 20px;">
        <tr>
            <td align="center">
                
                <table border="0" cellpadding="2" cellspacing="0" width="100%%" style="max-width: 450px; background-color: #c0c0c0; border-top: 2px solid #ffffff; border-left: 2px solid #ffffff; border-bottom: 2px solid #000000; border-right: 2px solid #000000;">
                    <tr>
                        <td>
                            
                            <table border="0" cellpadding="4" cellspacing="0" width="100%%" style="background-color: #000080; border: 1px solid #c0c0c0;">
                                <tr>
                                    <td style="color: #ffffff; font-weight: bold; font-size: 13px; font-family: 'MS Sans Serif', Tahoma, sans-serif; letter-spacing: 0.5px;">
                                        Verificacao_de_Email.exe
                                    </td>
                                    <td align="right" width="20">
                                        <table border="0" cellpadding="0" cellspacing="0" style="background-color: #c0c0c0; border-top: 1px solid #ffffff; border-left: 1px solid #ffffff; border-bottom: 1px solid #000000; border-right: 1px solid #000000; height: 16px; width: 16px;">
                                            <tr>
                                                <td align="center" valign="middle" style="color: #000000; font-size: 10px; font-weight: bold; font-family: Arial, sans-serif; line-height: 1;">
                                                    X
                                                </td>
                                            </tr>
                                        </table>
                                    </td>
                                </tr>
                            </table>

                            <table border="0" cellpadding="15" cellspacing="0" width="100%%">
                                <tr>
                                    <td style="color: #000000; font-size: 13px; font-family: 'MS Sans Serif', Tahoma, sans-serif; line-height: 1.5;">
                                        <p style="margin-top: 0;"><b>Bem-vindo(a), %[2]s!</b></p>
                                        <p>Para ativar sua conta no <b>%[1]s</b> e ter acesso ao sistema, é necessário confirmar que este endereço de e-mail é válido e pertence a você.</p>
                                        <p>Clique no botão abaixo para verificar sua conta:</p>

                                        <table border="0" cellpadding="0" cellspacing="0" width="100%%" style="margin: 25px 0;">
                                            <tr>
                                                <td align="center">
                                                    <table border="0" cellpadding="0" cellspacing="0" class="win-button" style="background-color: #c0c0c0; border-top: 2px solid #ffffff; border-left: 2px solid #ffffff; border-bottom: 2px solid #000000; border-right: 2px solid #000000;">
                                                        <tr>
                                                            <td align="center" style="padding: 5px 15px;">
                                                                <a href="%[3]s" target="_blank" style="text-decoration: none; color: #000000; font-weight: bold; font-size: 13px; font-family: 'MS Sans Serif', Tahoma, sans-serif; display: block;">
                                                                    &nbsp;VERIFICAR E-MAIL&nbsp;
                                                                </a>
                                                            </td>
                                                        </tr>
                                                    </table>
                                                </td>
                                            </tr>
                                        </table>

                                        <hr style="border: none; border-top: 1px solid #808080; border-bottom: 1px solid #ffffff; margin: 20px 0;">

                                        <p style="margin: 0; font-size: 11px; text-align: center; color: #555555;">
                                            Se o botão não funcionar, copie e cole este link no navegador:<br>
                                            <a href="%[3]s" style="color: #000080; text-decoration: underline; word-break: break-all;">
                                               %[3]s
                                            </a>
                                        </p>
                                    </td>
                                </tr>
                            </table>

                        </td>
                    </tr>
                </table>
                <p style="color: #ffffff; font-size: 11px; font-family: 'MS Sans Serif', Tahoma, sans-serif; text-align: center; margin-top: 20px;">
                    © 2006-2026 %[1]s. Todos os direitos reservados.
                </p>

            </td>
        </tr>
    </table>

</body>
</html>`, e.appName, username, verifyURL)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
