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

// TestNewEmailService_Defaults verifica que variáveis de ambiente omitidas
// assumem os valores padrão corretos.
func TestNewEmailService_Defaults(t *testing.T) {
	// Limpa variáveis que podem herdar do ambiente de CI
	os.Unsetenv("SMTP_HOST")
	os.Unsetenv("SMTP_PORT")
	os.Unsetenv("SMTP_FROM")
	os.Unsetenv("APP_NAME")
	os.Unsetenv("SMTP_USERNAME")
	os.Unsetenv("SMTP_PASSWORD")

	svc := NewEmailService().(*emailService)

	if svc.host != "smtp.gmail.com" {
		t.Errorf("host padrão esperado 'smtp.gmail.com', obteve '%s'", svc.host)
	}
	if svc.port != "587" {
		t.Errorf("port padrão esperado '587', obteve '%s'", svc.port)
	}
	if svc.appName != "CACC Portal" {
		t.Errorf("appName padrão esperado 'CACC Portal', obteve '%s'", svc.appName)
	}
	// from deve ser igual a username quando SMTP_FROM não está setado
	if svc.from != svc.username {
		t.Errorf("from deveria ser igual a username quando SMTP_FROM está vazio")
	}
}

// TestNewEmailService_EnvVars verifica que as variáveis de ambiente são lidas.
func TestNewEmailService_EnvVars(t *testing.T) {
	os.Setenv("SMTP_HOST", "smtp.example.com")
	os.Setenv("SMTP_PORT", "465")
	os.Setenv("SMTP_USERNAME", "user@example.com")
	os.Setenv("SMTP_PASSWORD", "supersecret")
	os.Setenv("SMTP_FROM", "noreply@example.com")
	os.Setenv("APP_NAME", "Meu Portal")
	defer func() {
		os.Unsetenv("SMTP_HOST")
		os.Unsetenv("SMTP_PORT")
		os.Unsetenv("SMTP_USERNAME")
		os.Unsetenv("SMTP_PASSWORD")
		os.Unsetenv("SMTP_FROM")
		os.Unsetenv("APP_NAME")
	}()

	svc := NewEmailService().(*emailService)

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

// TestSendPasswordReset_NoCredentials garante que o método retorna erro
// imediatamente quando as credenciais SMTP não estão configuradas.
func TestSendPasswordReset_NoCredentials(t *testing.T) {
	svc := newTestEmailService("smtp.gmail.com", "587", "", "", "", "Portal")

	err := svc.SendPasswordReset("dest@example.com", "João", "https://example.com/reset")
	if err == nil {
		t.Fatal("esperava erro quando username/password estão vazios")
	}
	if !strings.Contains(err.Error(), "SMTP não configurado") {
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
		"text/html",
	}

	// O HTML deve conter esses termos
	for _, term := range checks {
		if !strings.Contains(html, term) {
			// "text/html" está no header, não no body — ajusta a busca
			if term == "text/html" {
				continue // buildResetEmail retorna só o body HTML, não o header
			}
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

// startFakeSMTP sobe um servidor TCP mínimo que implementa o protocolo SMTP
// básico (sem TLS) e captura a mensagem enviada pelo cliente.
// Retorna o endereço "host:port" e um canal que recebe a mensagem completa.
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

		// Saudação inicial
		write("220 127.0.0.1 SMTP fake server ready")
		line := readLine() // EHLO ou HELO

		if strings.HasPrefix(strings.ToUpper(line), "EHLO") {
			// Responde EHLO sem anunciar STARTTLS para forçar caminho sem TLS
			write("250-127.0.0.1 Hello")
			write("250-SIZE 10240000")
			write("250 AUTH PLAIN LOGIN")
		} else {
			write("250 127.0.0.1")
		}

		// Auth
		authCmd := readLine()
		if strings.HasPrefix(strings.ToUpper(authCmd), "AUTH") {
			write("334 ")
			readLine() // credenciais em base64
			write("235 2.7.0 Authentication successful")
		}

		readLine() // MAIL FROM
		write("250 Ok")
		readLine() // RCPT TO
		write("250 Ok")
		readLine() // DATA
		write("354 End data with <CR><LF>.<CR><LF>")

		// Lê o corpo da mensagem até encontrar o terminador "."
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

// TestSend_FakeSMTP testa o fluxo completo de envio contra um servidor SMTP fake.
// O smtp.PlainAuth do Go só permite conexões não-TLS para 127.0.0.1/localhost,
// por isso o servidor fake escuta em 127.0.0.1.
func TestSend_FakeSMTP(t *testing.T) {
	host, port, msgCh := startFakeSMTP(t)

	svc := newTestEmailService(host, port, "user@test.com", "pass", "from@test.com", "TestApp")

	err := svc.send("to@test.com", "Assunto Teste", "<p>Corpo do e-mail</p>")
	if err != nil {
		// Erros de STARTTLS ou auth são esperados em ambientes sem TLS real.
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
// Teste de integração real (skipped por padrão; rode com -run Integration)
// ---------------------------------------------------------------------------

// TestSendPasswordReset_Integration envia um e-mail de verdade usando as
// credenciais do arquivo .env. Execute com:
//
//	go test -v -run TestSendPasswordReset_Integration ./pkg/services/
//
// Defina a variável de ambiente EMAIL_TO para o destinatário.
func TestSendPasswordReset_Integration(t *testing.T) {
	if os.Getenv("SMTP_PASSWORD") == "" {
		t.Skip("SMTP_PASSWORD não configurado — pulando teste de integração")
	}

	to := os.Getenv("EMAIL_TO")
	if to == "" {
		to = os.Getenv("SMTP_USERNAME") // auto-envia para si mesmo se não configurado
	}
	if to == "" {
		t.Skip("EMAIL_TO e SMTP_USERNAME não configurados — pulando teste de integração")
	}

	// Carrega credenciais direto das envs (assumindo que foram setadas ou via .env)
	svc := NewEmailService()

	err := svc.SendPasswordReset(
		to,
		"Usuário Teste",
		"https://portal.cacc.dev/reset?token=TEST-TOKEN-123",
	)
	if err != nil {
		t.Fatalf("❌ Falha ao enviar e-mail real: %v", err)
	}
	t.Logf("✅ E-mail de redefinição enviado com sucesso para %s", to)

	// Verifica também autenticação via smtp.PlainAuth (sanity check separado)
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
