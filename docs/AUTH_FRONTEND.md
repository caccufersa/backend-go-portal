# Documentação de Autenticação (Front-End)

Este documento descreve como o Front-End deve interagir com os novos fluxos de autenticação da API: **Login com Google** e **Redefinição de Senha via E-mail**.

Ambos os fluxos mantêm a compatibilidade com a estrutura de Tokens do sistema (Access Token retorna no corpo da resposta e Refresh Token num Cookie `HttpOnly`).

---

## 🔵 1. Login com Google (OAuth2)

O fluxo do Google usa redirecionamento (Padrão OAuth 2.0).

### Passo 1: Iniciar o Login
O usuário clica no botão "Entrar com Google" no Front-End.
O Front-End deve redirecionar (mudar a URL atual) o navegador do usuário ou abrir um popup apontando para o endpoint do Backend:

**`GET /auth/google`**

Isso fará com que o backend gere um cookie temporário de segurança (CSRF `oauth_state`) e redirecione automaticamente o usuário para a página de consentimento do Google.

### Passo 2: O Retorno do Google (Callback)
Após o usuário fazer login no Google, o Google o envia de volta para o Backend no endpoint `/auth/google/callback`.
O Backend fará toda a validação e obterá o perfil do usuário.

Em seguida, o Backend **redirecionará** o navegador de volta para o Front-End (URL mapeada na variável de ambiente `FRONTEND_URL` do Backend) da seguinte forma:

**`GET https://seu-frontend.com/auth/callback#access_token=eyJhbGciOi...`**

> **Importante:** O access token é passado em um **Fragment Hash (`#`)** da URL. Isso significa que ele *não será salvo no histórico do navegador* ou vazará em logs de rede, ao contrário de Query Parameters (`?`). O Refresh Token já terá sido setado magicamente via Cookie `HttpOnly`.

### Passo 3: Tratamento no Front-End
Você deve criar uma página ou componente no Front-End que atenda a rota `/auth/callback` (ex: em React/Next.js).

O Front-End deverá:
1. Extrair o `access_token` do Hash da URL (exemplo em JavaScript):
   ```javascript
   const hash = window.location.hash.substring(1); // remove o #
   const params = new URLSearchParams(hash);
   const accessToken = params.get("access_token");
   ```
2. Salvar o `accessToken` localmente (em memória ou contexto, evite `localStorage` para não permitir XSS se possível, mas use o padrão do projeto).
3. Disparar uma requisição para **`GET /auth/session`** do Backend, enviando o Access Token no Header (`Authorization: Bearer <token>`) para que o Backend retorne os dados bonitinhos do usuário (id, username, email) e atualize o Context de Auth.
4. Limpar a URL do navegador para remover o token da barra de endereços (ex: `window.history.replaceState({}, document.title, "/")`) e redirecionar para a *Home* ou *Dashboard*.

---

## 📧 2. Fluxo de Esqueci Minha Senha

O fluxo antigo usando "Recovery Key" enviada no login foi descartado. O sistema agora envia um **E-mail real** contendo um link temporário (expira em 15 minutos).

### Passo 1: Solicitar o E-mail
O usuário não lembra a senha. Ele clica em "Esqueci minha senha" e abre um modal pedindo o e-mail cadastrado.

**`POST /auth/forgot-password`**
```json
// Body da requisição (JSON)
{
  "email": "usuario@exemplo.com"
}
```
**Resposta do Backend:**
Independente de o e-mail existir na base (para evitar invasores de adivinhar usuários cadastrados), o Backend sempre retorna HTTP `200 OK`:
```json
{
  "message": "Se uma conta com esse e-mail existir, você receberá um link de redefinição em breve."
}
```
> O Front-End deve exibir essa exata mensagem e fechar o modal.

### Passo 2: O Usuário clica no Link do E-mail
O e-mail enviado ao usuário conterá um botão que aponta para uma página do Front-End montada a partir da variável `FRONTEND_URL`.

O usuário irá parar na URL do Front-End:
**`https://seu-frontend.com/reset-password?token=a1b2c3d4e5f6...`**

O Front-End precisará extrair a *query string* `token` da URL. (Neste caso é Query Params `?` normalmente).

### Passo 3: Enviar a Nova Senha pro Backend
Na página `/reset-password` do Front-End, apresente um campo de "Nova Senha". Quando o usuário preencher e salvar, você fará a chamada final para a API usando o token extraindo do passo 2:

**`POST /auth/reset-password`**
```json
// Body da requisição (JSON)
{
  "token": "token_extraido_da_url_do_email",
  "new_password": "NovaSenhaSegura123!"
}
```
**Respostas da requisição:**
- HTTP `200 OK` (Sucesso):
  ```json
  { "message": "Senha redefinida com sucesso. Faça login novamente." }
  ```
  O Front-End redireciona para a tela de Login normal (todas as sessões antigas que o usuário tinha em outros dispositivos serão deslogadas automaticamente por segurança).
- HTTP `400 Bad Request` (Token expirado ou inválido, senha com menos de 8 caracteres):
  ```json
  { "erro": "token expirado, solicite um novo" } // ou semelhantes
  ```

---

## 🔒 3. Resumo dos Demais Endpoints (Sem alterações estruturais)

- **`POST /auth/register`**: Pode opcionalmente incluir a chave `"email": "usuario@exemplo.com"` agora. O Backend irá salvar para habilitar recuperação via email. O email é obrigatório para ser possível recuperar a senha!
- **`POST /auth/login`**: Mantém `"username"` e `"password"`. Contas exclusivas do Google recebem erro alertando que a conta foi criada via Google.
- **`POST /auth/logout`**: Sem alterações (envia POST para invalidar sessão do banco).
- **`POST /auth/logout-all`**: Encerra ativamente todas as sessões em todos os dispositivos para o usuário atual.
- **`GET /auth/session`**: Mantém a lógica de validar cookies na reidratação/F5 do Front-End.
