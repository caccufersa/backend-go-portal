package services

import (
	"crypto/tls"
	"fmt"
	"net/smtp"
	"os"
)

// EmailService sends transactional emails via SMTP.
// All emails are sent through local SMTP server (Postfix in Docker)
// or external SMTP relay (Gmail, SendGrid, etc)
type EmailService interface {
	SendPasswordReset(toEmail, username, resetURL string) error
	SendEmailVerification(toEmail, username, verifyURL string) error
}

// ---------------------------------------------------------------------------
// Email service struct
// ---------------------------------------------------------------------------

type emailService struct {
	host     string
	port     string
	username string
	password string
	from     string
	appName  string
}

func NewEmailService() EmailService {
	appName := os.Getenv("APP_NAME")
	if appName == "" {
		appName = "CACC Portal"
	}

	svc := &emailService{
		appName:  appName,
		host:     envOrDefault("SMTP_HOST", "localhost"),
		port:     envOrDefault("SMTP_PORT", "587"),
		username: os.Getenv("SMTP_USERNAME"),
		password: os.Getenv("SMTP_PASSWORD"),
		from:     os.Getenv("SMTP_FROM"),
	}

	if svc.from == "" {
		svc.from = svc.username
		if svc.from == "" {
			svc.from = fmt.Sprintf("noreply@%s", os.Getenv("DOMAIN"))
		}
	}

	return svc
}

// ---------------------------------------------------------------------------
// Public methods
// ---------------------------------------------------------------------------

func (e *emailService) SendPasswordReset(toEmail, username, resetURL string) error {
	subject := fmt.Sprintf("Redefinição de senha – %s", e.appName)
	body := e.buildResetEmail(username, resetURL)

	return e.sendSMTP(toEmail, subject, body)
}

func (e *emailService) SendEmailVerification(toEmail, username, verifyURL string) error {
	subject := fmt.Sprintf("Verificação de E-mail – %s", e.appName)
	body := e.buildVerificationEmail(username, verifyURL)

	return e.sendSMTP(toEmail, subject, body)
}

// ---------------------------------------------------------------------------
// SMTP implementation
// ---------------------------------------------------------------------------

func (e *emailService) sendSMTP(to, subject, htmlBody string) error {
	if e.host == "" || e.port == "" {
		return fmt.Errorf("SMTP não configurado (SMTP_HOST/SMTP_PORT ausentes no ambiente)")
	}

	// Prepare message headers
	headers := fmt.Sprintf("From: <%s>\r\nTo: <%s>\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=utf-8\r\n\r\n", e.from, to, subject)
	body := headers + htmlBody

	// Connect to SMTP server with STARTTLS
	addr := fmt.Sprintf("%s:%s", e.host, e.port)
	conn, err := tls.Dial("tcp", addr, &tls.Config{
		ServerName: e.host,
		// InsecureSkipVerify for local testing, remove in production
		InsecureSkipVerify: false,
	})
	if err != nil {
		return fmt.Errorf("falha ao conectar ao servidor SMTP %s: %v", addr, err)
	}
	defer conn.Close()

	// Create SMTP client
	client, err := smtp.NewClient(conn, e.host)
	if err != nil {
		return fmt.Errorf("falha ao criar cliente SMTP: %v", err)
	}
	defer client.Quit()

	// Authenticate if credentials are provided
	if e.username != "" && e.password != "" {
		auth := smtp.PlainAuth("", e.username, e.password, e.host)
		if err = client.Auth(auth); err != nil {
			return fmt.Errorf("falha na autenticação SMTP: %v", err)
		}
	}

	// Send email
	if err = client.Mail(e.from); err != nil {
		return fmt.Errorf("falha ao definir remetente: %v", err)
	}

	if err = client.Rcpt(to); err != nil {
		return fmt.Errorf("falha ao definir destinatário: %v", err)
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("falha ao preparar dados: %v", err)
	}

	_, err = w.Write([]byte(body))
	if err != nil {
		return fmt.Errorf("falha ao escrever corpo do email: %v", err)
	}

	err = w.Close()
	if err != nil {
		return fmt.Errorf("falha ao fechar conexão de dados: %v", err)
	}

	return nil
}

// ---------------------------------------------------------------------------
// HTML templates
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
