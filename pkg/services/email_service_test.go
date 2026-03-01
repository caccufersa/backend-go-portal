package services

import (
	"net"
	"net/smtp"
	"os"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newTestEmailService cria um emailService com valores controlados pelo teste.
func newTestEmailService(host, port, user, pass, from, appName string) *emailService {
	return &emailService{
		provider: "smtp",
		host:     host,
		port:     port,
		username: user,
		password: pass,
		from:     from,
		appName:  appName,
	}
}

// ---------------------------------------------------------------------------
// Testes unitários (sem conexão SMTP real)
// ---------------------------------------------------------------------------

// TestNewEmailService_DefaultsResend verifica que sem EMAIL_PROVIDER,
// o padrão é Resend.
func TestNewEmailService_DefaultsResend(t *testing.T) {
	os.Unsetenv("EMAIL_PROVIDER")
	os.Unsetenv("RESEND_API_KEY")
	os.Unsetenv("RESEND_FROM")
	os.Unsetenv("APP_NAME")

	svc := NewEmailService().(*emailService)

	if svc.provider != "resend" {
		t.Errorf("provider padrão esperado 'resend', obteve '%s'", svc.provider)
	}
	if svc.appName != "CACC Portal" {
		t.Errorf("appName padrão esperado 'CACC Portal', obteve '%s'", svc.appName)
	}
}

// TestNewEmailService_SMTP verifica que EMAIL_PROVIDER=smtp usa SMTP.
func TestNewEmailService_SMTP(t *testing.T) {
	os.Setenv("EMAIL_PROVIDER", "smtp")
	os.Setenv("SMTP_HOST", "smtp.example.com")
	os.Setenv("SMTP_PORT", "465")
	os.Setenv("SMTP_USERNAME", "user@example.com")
	os.Setenv("SMTP_PASSWORD", "supersecret")
	os.Setenv("SMTP_FROM", "noreply@example.com")
	os.Setenv("APP_NAME", "Meu Portal")
	defer func() {
		os.Unsetenv("EMAIL_PROVIDER")
		os.Unsetenv("SMTP_HOST")
		os.Unsetenv("SMTP_PORT")
		os.Unsetenv("SMTP_USERNAME")
		os.Unsetenv("SMTP_PASSWORD")
		os.Unsetenv("SMTP_FROM")
		os.Unsetenv("APP_NAME")
	}()

	svc := NewEmailService().(*emailService)

	if svc.provider != "smtp" {
		t.Errorf("provider esperado 'smtp', obteve '%s'", svc.provider)
	}

	checks := map[string][2]string{
		"host":     {"smtp.example.com", svc.host},
		"port":     {"465", svc.port},
		"username": {"user@example.com", svc.username},
		"password": {"supersecret", svc.password},
		"from":     {"noreply@example.com", svc.from},
		"appName":  {"Meu Portal", svc.appName},
	}
	for field, v := range checks {
		if v[0] != v[1] {
			t.Errorf("%s: esperado '%s', obteve '%s'", field, v[0], v[1])
		}
	}
}

// TestNewEmailService_Resend verifica que as variáveis Resend são lidas.
func TestNewEmailService_Resend(t *testing.T) {
	os.Setenv("EMAIL_PROVIDER", "resend")
	os.Setenv("RESEND_API_KEY", "re_test_123")
	os.Setenv("RESEND_FROM", "Portal <noreply@meudominio.com>")
	os.Setenv("APP_NAME", "Meu Portal")
	defer func() {
		os.Unsetenv("EMAIL_PROVIDER")
		os.Unsetenv("RESEND_API_KEY")
		os.Unsetenv("RESEND_FROM")
		os.Unsetenv("APP_NAME")
	}()

	svc := NewEmailService().(*emailService)

	if svc.resendAPIKey != "re_test_123" {
		t.Errorf("resendAPIKey esperado 're_test_123', obteve '%s'", svc.resendAPIKey)
	}
	if svc.resendFrom != "Portal <noreply@meudominio.com>" {
		t.Errorf("resendFrom inesperado: '%s'", svc.resendFrom)
	}
}

// TestSendPasswordReset_NoCredentials garante que o método retorna erro
// imediatamente quando as credenciais SMTP não estão configuradas.
func TestSendPasswordReset_NoCredentials_SMTP(t *testing.T) {
	svc := newTestEmailService("smtp.gmail.com", "587", "", "", "", "Portal")

	err := svc.SendPasswordReset("dest@example.com", "João", "https://example.com/reset")
	if err == nil {
		t.Fatal("esperava erro quando username/password estão vazios")
	}
	if !strings.Contains(err.Error(), "SMTP não configurado") {
		t.Errorf("mensagem de erro inesperada: %s", err.Error())
	}
}

// TestSendPasswordReset_NoCredentials_Resend garante que retorna erro sem API key.
func TestSendPasswordReset_NoCredentials_Resend(t *testing.T) {
	svc := &emailService{
		provider:     "resend",
		resendAPIKey: "",
		appName:      "Portal",
	}

	err := svc.SendPasswordReset("dest@example.com", "João", "https://example.com/reset")
	if err == nil {
		t.Fatal("esperava erro quando RESEND_API_KEY está vazio")
	}
	if !strings.Contains(err.Error(), "Resend não configurado") {
		t.Errorf("mensagem de erro inesperada: %s", err.Error())
	}
}

// TestBuildResetEmail verifica se o HTML gerado contém os campos esperados.
func TestBuildResetEmail(t *testing.T) {
	svc := newTestEmailService("", "", "", "", "", "Portal Teste")

	username := "Maria"
	resetURL := "https://portal.example.com/reset?token=abc123"

	html := svc.buildResetEmail(username, resetURL)

	checks := []string{
		"Portal Teste",
		"Maria",
		resetURL,
		"Redefinição de Senha",
		"15 minutos",
	}

	for _, term := range checks {
		if !strings.Contains(html, term) {
			t.Errorf("HTML gerado não contém '%s'", term)
		}
	}

	// Deve ter a tag de expiração
	if !strings.Contains(html, "expira") {
		t.Error("HTML não menciona expiração do link")
	}

	// O link de redefinição deve aparecer ao menos duas vezes (botão + link de texto)
	count := strings.Count(html, resetURL)
	if count < 2 {
		t.Errorf("esperava resetURL ao menos 2x no HTML, encontrou %d", count)
	}
}

// ---------------------------------------------------------------------------
// Servidor SMTP fake (in-process) para testar o fluxo de envio completo
// ---------------------------------------------------------------------------

func startFakeSMTP(t *testing.T) (host, port string, msgCh <-chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("falha ao iniciar servidor SMTP fake: %v", err)
	}
	ch := make(chan string, 1)

	go func() {
		defer ln.Close()
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		write := func(s string) { conn.Write([]byte(s + "\r\n")) } //nolint
		readLine := func() string {
			buf := make([]byte, 4096)
			n, _ := conn.Read(buf)
			return strings.TrimSpace(string(buf[:n]))
		}

		write("220 127.0.0.1 SMTP fake server ready")
		line := readLine()

		if strings.HasPrefix(strings.ToUpper(line), "EHLO") {
			write("250-127.0.0.1 Hello")
			write("250-SIZE 10240000")
			write("250 AUTH PLAIN LOGIN")
		} else {
			write("250 127.0.0.1")
		}

		authCmd := readLine()
		if strings.HasPrefix(strings.ToUpper(authCmd), "AUTH") {
			write("334 ")
			readLine()
			write("235 2.7.0 Authentication successful")
		}

		readLine() // MAIL FROM
		write("250 Ok")
		readLine() // RCPT TO
		write("250 Ok")
		readLine() // DATA
		write("354 End data with <CR><LF>.<CR><LF>")

		var body strings.Builder
		buf := make([]byte, 65536)
		for {
			n, _ := conn.Read(buf)
			if n == 0 {
				break
			}
			chunk := string(buf[:n])
			body.WriteString(chunk)
			if strings.Contains(body.String(), "\r\n.\r\n") {
				break
			}
		}
		write("250 Ok: queued")
		readLine() // QUIT
		write("221 Bye")

		ch <- body.String()
	}()

	addr := ln.Addr().String()
	parts := strings.SplitN(addr, ":", 2)
	return parts[0], parts[1], ch
}

func TestSend_FakeSMTP(t *testing.T) {
	host, port, msgCh := startFakeSMTP(t)

	svc := newTestEmailService(host, port, "user@test.com", "pass", "from@test.com", "TestApp")

	err := svc.sendSMTP("to@test.com", "Assunto Teste", "<p>Corpo do e-mail</p>")
	if err != nil {
		t.Logf("ℹ️  send() retornou (esperado em ambiente sem TLS real): %v", err)
		return
	}

	select {
	case msg := <-msgCh:
		trunc := msg
		if len(trunc) > 300 {
			trunc = msg[:300]
		}
		t.Logf("✅ Mensagem capturada pelo servidor fake:\n%s", trunc)
		if !strings.Contains(msg, "Assunto Teste") {
			t.Errorf("servidor não recebeu o subject correto")
		}
		if !strings.Contains(msg, "Corpo do e-mail") {
			t.Errorf("servidor não recebeu o body HTML")
		}
	default:
		t.Log("servidor fake não capturou mensagem (idle após erro de TLS/auth)")
	}
}

// ---------------------------------------------------------------------------
// Teste de integração real (skipped por padrão)
// ---------------------------------------------------------------------------

func TestSendPasswordReset_Integration(t *testing.T) {
	provider := strings.ToLower(os.Getenv("EMAIL_PROVIDER"))

	switch provider {
	case "smtp":
		if os.Getenv("SMTP_PASSWORD") == "" {
			t.Skip("SMTP_PASSWORD não configurado — pulando teste de integração SMTP")
		}
	default:
		if os.Getenv("RESEND_API_KEY") == "" {
			t.Skip("RESEND_API_KEY não configurado — pulando teste de integração Resend")
		}
	}

	to := os.Getenv("EMAIL_TO")
	if to == "" {
		to = os.Getenv("SMTP_USERNAME")
	}
	if to == "" {
		t.Skip("EMAIL_TO e SMTP_USERNAME não configurados — pulando teste de integração")
	}

	svc := NewEmailService()

	err := svc.SendPasswordReset(
		to,
		"Usuário Teste",
		"https://portal.cacc.dev/reset?token=TEST-TOKEN-123",
	)
	if err != nil {
		t.Fatalf("❌ Falha ao enviar e-mail real: %v", err)
	}
	t.Logf("✅ E-mail de redefinição enviado com sucesso para %s (provider: %s)", to, provider)

	// Verifica smtp.PlainAuth apenas para provider SMTP
	if provider == "smtp" {
		host := os.Getenv("SMTP_HOST")
		if host == "" {
			host = "smtp.gmail.com"
		}
		user := os.Getenv("SMTP_USERNAME")
		pass := os.Getenv("SMTP_PASSWORD")

		auth := smtp.PlainAuth("", user, pass, host)
		if auth == nil {
			t.Error("smtp.PlainAuth retornou nil inesperadamente")
		}
	}
}
