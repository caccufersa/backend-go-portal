# Integração do Frontend: Backoffice Administrativo 🛠️

Este documento contém o guia definitivo para o time de Frontend implementar as requisições, endpoints e tipagens (TypeScript) do painel administrativo do CACC.

Todas as rotas detalhadas aqui exigem **nível de acesso administrativo**, o que significa que duas regras de cabeçalho (`Headers`) devem ser estritamente cumpridas.

---

## 1. Regras de Autenticação (Headers)

Para que o backend aceite requisições mutáveis (criar, editar, deletar) na área do painel, você **precisa** injetar os seguintes `headers` (cabecalhos) no seu *Axios* ou *Fetch*:

```typescript
const adminHeaders = {
  "Content-Type": "application/json",
  "Authorization": `Bearer ${SUA_VARIAVEL_DE_TOKEN_JWT_AQUI}`,
  "X-Admin-Key": import.meta.env.VITE_ADMIN_SECRET_KEY // ou a senha definida no dev
};
```
> ⚠️ **Atenção**: Se bater num endpoint usando apenas o `Authorization`, receberá `403 Forbidden` informando que falta a Secret Key. A chave em modo de desenvolvimento local é: `"dev-admin-secret"`.

---

## 2. Tipagens / Interfaces (TypeScript)

Use essas interfaces na sua pasta de `models/` do React ou aplicativo Angular/Vue para garantir inteligência da IDE.

### 📌 Notícias (News)
```typescript
interface Noticia {
  id: number;
  titulo: string;
  conteudo: string | any; // Pode ser Markdown ou EditorJS Block Obj
  resumo: string;
  author: string;
  categoria: string;
  image_url?: string;
  destaque: boolean;
  tags?: string[];
  created_at: string; // ISO 8601 Date
  updated_at: string;
}

interface CriarNoticiaRequest {
  titulo: string;
  conteudo: string | any;
  resumo: string;
  author: string;
  categoria: string;
  image_url?: string;
  destaque: boolean;
  tags?: string[];
}

interface AtualizarNoticiaRequest {
  titulo?: string;
  conteudo?: string | any;
  resumo?: string;
  categoria?: string;
  image_url?: string;
  destaque?: boolean;
  tags?: string[];
}
```

### 📌 Sugestões
```typescript
interface Sugestao {
  id: number;
  texto: string;
  created_at: string;
  author: string;
  categoria: string;
}

interface SugestaoUpdateRequest {
  texto: string;
  categoria: string;
}
```

### 📌 Viagens de Ônibus (Bus Trips)
```typescript
interface BusTrip {
  id: string; // Ex: "mossoro-natal-10"
  name: string; // Ex: "Expedição Campus Central"
  description: string;
  departure_time?: string; // ISO Date ou null
  total_seats: number;
  is_completed: boolean;
  created_at: string;
  updated_at: string;
}

interface TripCreateRequest {
  id: string; 
  name: string;
  description: string;
  total_seats: number; // Dispara a geração automática destas poltronas no DB
  departure_time?: string;
}

interface TripUpdateRequest {
  name: string;
  description: string;
  is_completed?: boolean; // Útil para fechar a viagem e bloquear reservas
  departure_time?: string;
}
```

---

## 3. Endpoints da API

Abaixo está o dicionário de APIs expostas. **Lembre-se:** Você sempre chamará essas rotas prefixando em sua `BASE_URL` (Ex: `http://localhost:8082`).

### � Módulo de Notícias
A leitura das noticias `GET /noticias` não precisa de autenticação. Administradores focam em escrever:

#### Criar Nova Notícia
* **Endpoint**: `POST /noticias`
* **Headers**: `Authorization`, `X-Admin-Key`
* **Body**: `CriarNoticiaRequest`
* **Retorno (201)**: Objeto `Noticia` montado.

#### Editar Notícia
* **Endpoint**: `PUT /noticias/:id`
* **Headers**: `Authorization`, `X-Admin-Key`
* **Body**: `AtualizarNoticiaRequest` (apenas envie as chaves que deseja mudar)
* **Retorno (200)**: `{"status": "updated"}`

#### Deletar Notícia
* **Endpoint**: `DELETE /noticias/:id`
* **Headers**: `Authorization`, `X-Admin-Key`
* **Retorno (200)**: `{"status": "deleted"}`

---

### 💡 Módulo de Sugestões
Como leitura já era aberta `GET /sugestoes` e a submissão `POST /sugestoes` só requer login, restou ao backoffice apagar e editar:

#### Atualizar Sugestão 
Ideal se o usuário postar com a Categoria errada ou houver necessidade de moderar ofensas no painel:
* **Endpoint**: `PUT /sugestoes/:id`
* **Headers**: `Authorization`, `X-Admin-Key`
* **Body**: `SugestaoUpdateRequest`
* **Retorno (200)**: `{"status": "updated"}`

#### Excluir Sugestão Permanente
* **Endpoint**: `DELETE /sugestoes/:id`
* **Headers**: `Authorization`, `X-Admin-Key`
* **Retorno (200)**: `{"status": "deleted"}`

---

### 🚌 Módulo de Viagens de Ônibus
Leitura de usuários usa o endpoint: `GET /bus/trips` (Lista as viagens ativas).

#### Criar Rota / Viagem
A mágica acontece aqui. Ao criar, o banco automaticamente constroi os assentos com as configurações do `total_seats`.
* **Endpoint**: `POST /bus/trips`
* **Headers**: `Authorization`, `X-Admin-Key`
* **Body**: `TripCreateRequest`
* **Retorno (201)**: Devolve o objeto `BusTrip` recém criado.

#### Alterar Rota Existente
Use essa rota se precisar adiar o `departure_time` (Horário da viagem) ou marcar `is_completed: true` quando o ônibus der partida.
* **Endpoint**: `PUT /bus/trips/:id` *(Onde ID é a string textual ou uuid da viagem)*
* **Headers**: `Authorization`, `X-Admin-Key`
* **Body**: `TripUpdateRequest`
* **Retorno (200)**: `{"status": "updated"}`

#### Excluir Rota / Viagem 
**Cuidado**: Ao disparar essa rota, todas as reservas (`bus_seats`) cadastradas atreladas a esta viagem desaparecerão permanentemente pelo comportamento `CASCADE` do banco de dados relacional.
* **Endpoint**: `DELETE /bus/trips/:id`
* **Headers**: `Authorization`, `X-Admin-Key`
* **Retorno (200)**: `{"status": "deleted"}`

---

## 4. Retornos HTTP (Lista de Tratamentos Erros)
Sempre em seu Frontend implemente um `try { ... } catch (error)` que lide com as seguintes premissas padronizadas em todas as rotas acima:
- **`200 OK` / `201 CREATED`**: Sucesso Absoluto
- **`400 Bad Request`**: JSON enviezado ou dados faltando
- **`401 Unauthorized`**: Token JWT Invalido ou Espirado
- **`403 Forbidden`**: Errou a Chave Secreta `X-Admin-Key`
- **`500 Internal Server Error`**: O Banco de Dados caiu ou os caches explodiram
