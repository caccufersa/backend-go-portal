# Documentação da API - Integração Backoffice Admin

O Backoffice Administrativo do Portal do CACC provê recursos mutáveis restritos para diretores e membros da gestão administrarem conteúdo estático no site. Todas as rotas citadas neste documento que alteram dados exigem o Middleware `X-Admin-Key` em desenvolvimento (se configurado) ou estritamente a dupla de cabeçalhos de Autorização ativada.

Para acessar via FrontEnd/Client do Backoffice React:
1. Obter o seu Token bearer fazendo um POST de login no endpoint comum.
2. Inserir Header obrigatório em todas requisições não listadas como públicas: `Authorization: Bearer <SEU_TOKEN_JWT>`.
3. (Secundário se a .env ADMIN_KEY estiver ligada): Repassar um Header genérico adicional `X-Admin-Key: chave_configurada`.

---

## 🚌 1. Reserva de Ônibus (Administração de Viagens)
_Permite listar, criar viagens personalizadas com limitação de assentos e manipular a disponibilidade global para alunos._

### 1.1 Listar Viagens (Tanto públicas quanto ocultas/incompletas)
- **Método:** `GET`
- **Rota:** `/bus/trips`
- **Autenticação:** Pública ou Privada.
- **Tipagem de Retorno (Array de Viagens):**
```typescript
interface BusTrip {
  id: string;                 // Nome simples referencial de URL, ex: "viagem-festa"
  name: string;               // Título que irá aparecer na UI do ônibus
  description: string;        // Textão com detalhes do evento
  departure_time?: string;    // Timestamp do dia/hora de saída
  total_seats: number;        // Lotação total física de slots
  is_completed: boolean;      // Bloqueador; Evita reservas novas se TRUE
  created_at: string;
  updated_at: string;
}
```

### 1.2 Criar Nova Viagem
Para criar um evento como calouradas onde um ônibus próprio foi pago.
- **Método:** `POST`
- **Rota:** `/bus/trips`
- **Nível Auth Requerido:** JWT Bearer (Logado) com flag interna `Admin`.
- **Payload Request:**
```json
{
  "id": "t10",
  "name": "Viagem UFC Natal",
  "description": "Excursão de desenvolvimento de sistemas...",
  "total_seats": 20,
  "departure_time": "2026-03-22T19:00:20Z"
}
```

### 1.3 Editar uma Viagem Existente
Pode corrigir lotação, horários, e **marcar a viagem como Completa (que pausa as inserções no frontend)**.
- **Método:** `PUT`
- **Rota:** `/bus/trips/:id` (Substitua `:id` pelo `id` do Request acima).
- **Nível Auth Requerido:** JWT Bearer + Admin.
- **Payload Request:** (Tudo aqui é Partial, mande apenas o que for mudar. Por exemplo forçar o esgotamento manual mudando a property `is_completed: true`):
```json
{
  "name": "Novo nome",
  "is_completed": true 
}
```

### 1.4 Apagar Viagem
Apaga de uma vez a viagem com todas as reservas encadeadas nela (Use com cuidado!).
- **Método:** `DELETE`
- **Rota:** `/bus/trips/:id`
- **Nível Auth Requerido:** JWT Bearer + Admin.
- **Retorno:** `{"status": "deleted"}`

---

## 📰 2. Notícias e Editorias
_Cria as notícias ou banners para a FrontPage e murais do site do CACC usando blocos ricos._

### 2.1 Criar Notícia (EditorJS Compatible)
- **Método:** `POST`
- **Rota:** `/noticias`
- **Nível Auth Requerido:** JWT Bearer + Admin.
- **Dica:** O Body recebe no `conteudo` qualquer JSON gerado pelo "Editor.js" ou Quill no Frontend do Admin e ele guarda encodado no banco de forma polimórfica (ou um Objeto rico ou uma String limpa de HTML fallback).
- **Payload Request:**
```json
{
  "titulo": "UFERSA assina parcerias CACC",
  "resumo": "Breve sinopse usada em listagens mobile",
  "conteudo": { "time": 1700, "blocks": [{"type": "paragraph", "data": {"text": "Tão legal"}}] },
  "author": "Secretário Exemplo",
  "categoria": "Academico",
  "image_url": "https://imgur.com/logo.png",
  "destaque": true,
  "tags": ["estudo", "ufrn", "festa"]
}
```

### 2.2 Editar Notícia Específica
Se um Diretor de Marketing errou o texto ou quer mudar a Imagem/Destaque do Front, mande requisição de Patch semântico.
- **Método:** `PUT`
- **Rota:** `/noticias/:id` (`id` numérico gerado no banco)
- **Nível Auth Requerido:** JWT Bearer + Admin.
- **Payload Request:** Iguais de postagem, mas em opcionais.

### 2.3 Deletar Notícia
Remove um banner ou notícia do mural global para sempre.
- **Método:** `DELETE`
- **Rota:** `/noticias/:id`
- **Nível Auth Requerido:** JWT Bearer + Admin.

---

## 💡 3. Sugestões e Reclamações (Review)
_A aba onde o corpo diretivo vê as dores dos estudantes enviadas pela Home do site_

### 3.1 Listar Sugestões (Leitura)
Mapeia a Caixa de Sugestões de todos os alunos enviadas do Fórum Frontend:
- **Método:** `GET`
- **Rota:** `/sugestoes`
- **Retorno:**
```typescript
interface Sugestao {
  id: number;
  texto: string;
  categoria: string;    // Ex: "Problemas do PPCC"
  author: string;       // username ou "Anônimo"
  created_at: string;
}
```

### 3.2 Apagar Sugestões Avaliadas 
Você pode usar a interface admin para dar "Baixa" e sumir com denúncias ou sugestões já resolvidas:
- **Método:** `DELETE`
- **Rota:** `/sugestoes/:id`
- **Nível Auth Requerido:** JWT Bearer + Admin.

## Considerações Globais (Axios)
Crie um Interceptor do Axios ou `fetch` wrapper na build da pasta /admin que automaticamente capture e plugue do contexto o Bearer token quando os usuários logarem lá dentro e faça a checagem de respostas API code `401 Unauthorized` retornando a UI de tela cinza de Login novamente. Seu endpoint para logar é o próprio `POST /auth/login`.
