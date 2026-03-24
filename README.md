# Portal CACC Backend — Arquitetura Atual (2026)

Documentação técnica consolidada da arquitetura **real** implementada no backend Go (`module cacc`), com foco em camadas, fluxos de negócio, modelos e integrações.

---

## 1) Visão de Contexto (C4-like)

```mermaid
flowchart LR
    FE[Frontend Web
    Next.js/Vite] -->|HTTP REST + JWT| API[Fiber API
    cmd/server/main.go]
    FE -->|WebSocket /ws| WS[WebSocket Gateway
    pkg/hub + pkg/envelope]

    API --> SVC[Services
    Regras de negócio]
    SVC --> REPO[Repositories
    Acesso a dados]

    REPO --> PG[(PostgreSQL)]
    SVC --> REDIS[(Redis Cache)]
    SVC --> MAIL[Email Provider
    SMTP ou Resend]
    SVC --> OAUTH[Google OAuth UserInfo]
    SVC --> CLOUD[Cloudinary Upload/Delete]

    WS --> HUB[Hub Broadcast/Reply]
    HUB --> FE
```

---

## 2) Arquitetura em Camadas (Código)

```mermaid
flowchart TB
    subgraph EntryPoints[Entry Points]
      MAIN[cmd/server/main.go]
      HTTP[handlers/*]
      WSGW[/ws + parseWSToken]
    end

    subgraph AppLayer[Application Layer]
      AUTHSVC[services/auth_service.go]
      SOCIALSVC[services/social_service.go]
      NOTISVC[(notification flow via social + repo)]
      NEWSSVC[services/noticias_service.go]
      SUGSVC[services/sugestoes_service.go]
      BUSSVC[services/bus_service.go]
      GALSVC[services/galeria_service.go]
      EMAILSVC[services/email_service.go]
    end

    subgraph DataLayer[Data Layer]
      AUTHREPO[repository/auth_repository.go]
      SOCIALREPO[repository/social_repository.go]
      NOTIREPO[repository/notification_repository.go]
      NEWSREPO[repository/noticias_repository.go]
      SUGREPO[repository/sugestoes_repository.go]
      BUSREPO[repository/bus_repository.go]
      GALREPO[repository/galeria_repository.go]
    end

    subgraph Infra[Infra]
      MID[middleware/*]
      CACHE[pkg/cache/redis]
      DB[pkg/database/postgres]
      HUB[pkg/hub]
      ENV[pkg/envelope]
    end

    MAIN --> MID
    MAIN --> HTTP
    MAIN --> WSGW

    HTTP --> AUTHSVC
    HTTP --> SOCIALSVC
    HTTP --> NEWSSVC
    HTTP --> SUGSVC
    HTTP --> BUSSVC
    HTTP --> GALSVC
    HTTP --> NOTIREPO

    AUTHSVC --> AUTHREPO
    AUTHSVC --> EMAILSVC

    SOCIALSVC --> SOCIALREPO
    SOCIALSVC --> AUTHREPO
    SOCIALSVC --> NOTIREPO

    NEWSSVC --> NEWSREPO
    SUGSVC --> SUGREPO
    BUSSVC --> BUSREPO
    GALSVC --> GALREPO
    GALSVC --> SOCIALREPO

    AUTHREPO --> DB
    SOCIALREPO --> DB
    NOTIREPO --> DB
    NEWSREPO --> DB
    SUGREPO --> DB
    BUSREPO --> DB
    GALREPO --> DB

    SOCIALSVC --> CACHE
    NEWSSVC --> CACHE
    SUGSVC --> CACHE
    BUSSVC --> CACHE
    MAIN --> HUB
    HUB --> ENV
```

---

## 3) Boot e Ciclo de Inicialização

```mermaid
sequenceDiagram
    participant M as main.go
    participant MID as middleware.InitSecrets
    participant PG as PostgreSQL
    participant R as Redis
    participant H as Hub
    participant A as App Fiber

    M->>MID: Injeta JWT_SECRET + ADMIN_SECRET_KEY
    M->>PG: Connect() + initDB(schema)
    M->>M: Configura pool SQL + goroutine cleanExpiredSessions()
    M->>R: cache.New() + Ping
    M->>H: hub.New()
    M->>M: Wire de repositórios/serviços/handlers
    M->>A: server.NewApp() + middlewares globais
    M->>A: Registra rotas REST e /ws
    M->>A: Listen(0.0.0.0:8082)
```

---

## 4) Mapa de APIs REST (grupos atuais)

```mermaid
mindmap
  root((API REST))
    /health
    /auth
      POST /register
      GET /verify-email
      POST /login
      POST /forgot-password
      POST /reset-password
      GET /google
      GET /google/callback
      POST /refresh
      GET /session
      GET /me (auth)
      POST /logout (auth)
      POST /logout-all (auth)
      GET /sessions (auth)
    /internal
      GET /user/:uuid
    /noticias
      GET /destaques
      GET /:id
      GET /
      POST / (auth+admin)
      PUT /:id (auth+admin)
      DELETE /:id (auth+admin)
    /sugestoes
      GET /
      POST / (optional auth)
      PUT /:id (auth+admin)
      DELETE /:id (auth+admin)
    /social
      GET /feed (optional auth)
      GET /feed/:id (optional auth)
      GET /profile/:username? (optional auth)
      PUT /profile (auth)
      POST /feed (auth)
      POST /feed/:id/reply (auth)
      PUT /feed/:id/like (auth)
      DELETE /feed/:id/like (auth)
      DELETE /feed/:id (auth)
    /bus
      GET /trips
      GET /:id/seats
      POST /trips (auth+admin)
      PUT /trips/:id (auth+admin)
      DELETE /trips/:id (auth+admin)
      POST /reserve (auth)
      POST /cancel (auth)
      GET /me (auth)
      GET /contact (auth)
      PUT /contact (auth)
    /notifications
      GET / (auth)
      PUT /read (auth)
    /galeria
      GET /list
      POST /upload (auth)
      DELETE /:id (auth)
```

---

## 5) Fluxo de Autenticação e Sessão

```mermaid
sequenceDiagram
    participant FE as Frontend
    participant AH as AuthHandler
    participant AS as AuthService
    participant AR as AuthRepository
    participant DB as PostgreSQL
    participant EM as Email

    FE->>AH: POST /auth/register
    AH->>AS: Register(req)
    AS->>AR: CreateUser + CreateEmailVerificationToken(hash)
    AR->>DB: INSERT users + email_verification_tokens
    AS-->>EM: SendEmailVerification(async)
    AH-->>FE: 201 + mensagem de verificação

    FE->>AH: GET /auth/verify-email?token
    AH->>AS: VerifyEmail(token)
    AS->>AR: GetEmailVerificationToken(hash) + VerifyEmail(user)
    AR->>DB: UPDATE users.is_verified=true
    AH-->>FE: 200

    FE->>AH: POST /auth/login
    AH->>AS: Login(username,password)
    AS->>AR: GetUserByUsername + CreateSession(hash)
    AR->>DB: INSERT sessions
    AH-->>FE: access_token + cookie refresh_token

    FE->>AH: POST /auth/refresh
    AH->>AS: Refresh(refresh_token)
    AS->>AR: GetSessionByToken(hash) + UpdateSession(new hash)
    AH-->>FE: novo access + novo refresh
```

### OAuth Google

```mermaid
sequenceDiagram
    participant FE as Frontend
    participant API as /auth/google
    participant GG as Google OAuth
    participant AS as AuthService
    participant DB as PostgreSQL

    FE->>API: GET /auth/google
    API-->>FE: Redirect Google + cookie oauth_state
    FE->>GG: Consent
    GG-->>API: /auth/google/callback?code&state
    API->>AS: GoogleCallback(code)
    AS->>GG: userinfo (Bearer access token)
    AS->>DB: GetOrCreateGoogleUser + CreateSession
    API-->>FE: Redirect FRONTEND_URL/auth/callback#access_token=...
```

---

## 6) Fluxo Social + Notificações

```mermaid
sequenceDiagram
    participant FE as Frontend
    participant SH as SocialHandler
    participant SS as SocialService
    participant SR as SocialRepository
    participant NR as NotificationRepository
    participant DB as PostgreSQL
    participant HUB as WebSocket Hub

    FE->>SH: POST /social/feed/:id/reply
    SH->>SS: CreateReply()
    SS->>SR: CreateReply + IncrementReplyCount + Thread(parent)
    SR->>DB: INSERT/UPDATE/SELECT posts

    alt dono do post diferente do ator
      SS->>NR: CreateNotification(type=reply)
      NR->>DB: INSERT notifications
    end

    SS->>NR: CreateNotification(type=mention) [se @mentions]
    SS->>HUB: Broadcast(new_reply)
    SH-->>FE: 201 reply
```

```mermaid
sequenceDiagram
    participant FE as Frontend
    participant NH as NotificationHandler
    participant NR as NotificationRepository
    participant DB as PostgreSQL

    FE->>NH: GET /notifications?limit&offset
    NH->>NR: GetNotifications(user)
    NR->>DB: SELECT notifications + actor join
    NH->>NR: MarkAsRead(user) (goroutine)
    NR->>DB: UPDATE notifications SET is_read=true
    NH-->>FE: []Notification
```

Tipos atualmente emitidos: `reply`, `repost`, `mention`, `like`.

---

## 7) WebSocket: Envelope e Broadcast

```mermaid
flowchart LR
    C[Cliente WS] -->|JSON Envelope| H[Hub.HandleClientConn]
    H -->|action=ping| C
    H -->|injeta user_id/uuid/username| ACT[Action Handler]
    ACT -->|Reply / ReplyError| H
    H --> C
    H -->|Broadcast| ALL[Todas conexões]
```

```mermaid
classDiagram
    class Envelope {
      +string id
      +string action
      +string service
      +int user_id
      +string user_uuid
      +string username
      +string reply_to
      +json data
      +ErrorPayload error
      +int64 ts
    }
    class ErrorPayload {
      +int code
      +string message
    }
    Envelope --> ErrorPayload
```

Eventos observados em produção de código:
- `user_login`, `user_logout`
- `new_post`, `new_reply`, `post_liked`, `post_deleted`, `profile_updated`
- `userCount`

---

## 8) Modelo de Dados (PostgreSQL)

```mermaid
erDiagram
    users ||--o{ sessions : has
    users ||--o| social_profiles : owns
    users ||--o{ posts : creates
    users ||--o{ notifications : receives
    users ||--o{ notifications : acts_as_actor
    users ||--o{ bus_seats : reserves
    users ||--o| bus_profiles : owns
    users ||--o{ galeria : uploads

    posts ||--o{ posts : replies_to
    posts ||--o{ post_likes : liked_by
    users ||--o{ post_likes : likes
    posts ||--o{ notifications : references

    bus_trips ||--o{ bus_seats : contains

    users {
      int id PK
      uuid uuid UK
      text username UK
      text email UK_nullable
      text password
      text google_id UK_nullable
      bool is_verified
      timestamp created_at
    }

    sessions {
      int id PK
      int user_id FK
      text refresh_token UK_hashed
      text user_agent
      text ip
      timestamp expires_at
      timestamp created_at
    }

    posts {
      int id PK
      text texto
      text author
      int user_id FK_nullable
      int parent_id FK_nullable
      int repost_id FK_nullable
      int likes
      int reply_count
      timestamp created_at
    }

    notifications {
      int id PK
      int user_id FK
      int actor_id FK_nullable
      text type
      int post_id FK_nullable
      bool is_read
      timestamp created_at
    }

    noticias {
      int id PK
      text titulo
      text conteudo
      text resumo
      text author
      text categoria
      text image_url
      bool destaque
      text[] tags
      timestamp created_at
      timestamp updated_at
    }

    sugestoes {
      int id PK
      text texto
      text author
      text categoria
      timestamp data_criacao
    }

    bus_trips {
      text id PK
      text name
      text description
      timestamp departure_time
      int total_seats
      bool is_completed
      timestamp created_at
      timestamp updated_at
    }

    bus_seats {
      text trip_id FK
      int seat_number
      int user_id FK_nullable
      timestamp reserved_at
      PK trip_id_seat_number
    }

    galeria {
      int id PK
      int user_id FK
      text author
      text author_name
      text avatar_url
      text image_url
      text public_id
      text caption
      timestamp created_at
    }
```

---

## 9) Modelos de Domínio (Go `pkg/models`)

```mermaid
classDiagram
    class User {
      +int ID
      +string UUID
      +string Username
      +string Email
      +bool IsVerified
      +string AvatarURL
      +string DisplayName
      +time CreatedAt
    }

    class Session {
      +int ID
      +int UserID
      +string UserAgent
      +string IP
      +time ExpiresAt
      +time CreatedAt
    }

    class Post {
      +int ID
      +string Texto
      +string Author
      +string AuthorName
      +string AvatarURL
      +int UserID
      +*int ParentID
      +*int RepostID
      +*Post Repost
      +int Likes
      +bool Liked
      +int ReplyCount
      +time CreatedAt
      +[]Post Replies
    }

    class Profile {
      +string Username
      +string DisplayName
      +string Bio
      +string AvatarURL
      +int TotalPosts
      +int TotalLikes
      +[]Post Posts
    }

    class Notification {
      +int ID
      +int UserID
      +*int ActorID
      +string ActorName
      +string ActorAvatar
      +string Type
      +*int PostID
      +bool IsRead
      +time CreatedAt
    }

    class Noticia {
      +int ID
      +string Titulo
      +string Conteudo
      +EditorJSData ConteudoObj
      +string Resumo
      +string Author
      +string Categoria
      +string ImageURL
      +bool Destaque
      +[]string Tags
      +time CreatedAt
      +time UpdatedAt
    }

    class Sugestao {
      +int ID
      +string Texto
      +time CreatedAt
      +string Author
      +string Categoria
    }

    class BusTrip {
      +string ID
      +string Name
      +string Description
      +time DepartureTime
      +int TotalSeats
      +bool IsCompleted
      +time CreatedAt
      +time UpdatedAt
    }

    class BusSeat {
      +string TripID
      +int SeatNumber
      +bool IsReserved
      +*int UserID
      +*time ReservedAt
    }

    class GaleriaItem {
      +int ID
      +int UserID
      +string Author
      +string AuthorName
      +string AvatarURL
      +string ImageURL
      +string PublicID
      +string Caption
      +time CreatedAt
    }

    User "1" --> "many" Session
    User "1" --> "many" Post
    User "1" --> "many" Notification
    Post "1" --> "many" Post : replies
```

---

## 10) Estratégia de Cache (Redis)

```mermaid
flowchart TB
    subgraph Social
      SF[social:feed:{limit}:{offset}:lid{user}]:::ttl15
      ST[social:thread:{post}:lid{user}]:::ttl30
      SP[social:profile:{user}:lid{requester}]:::ttl30
    end

    subgraph Noticias
      NL[noticias:list:{categoria}:{limit}:{offset}]:::ttl30
      NI[noticias:item:{id}]:::ttl60
      ND[noticias:destaques]:::ttl30
    end

    subgraph Sugestoes
      SA[sugestoes:all]:::ttl30
    end

    subgraph Bus
      BT[bus:trips:all]:::ttl300
      BS[bus:{trip}:seats]:::ttl1
    end

    classDef ttl1 fill:#e3f2fd,stroke:#1e88e5,color:#0d47a1;
    classDef ttl15 fill:#e8f5e9,stroke:#43a047,color:#1b5e20;
    classDef ttl30 fill:#fff3e0,stroke:#fb8c00,color:#e65100;
    classDef ttl60 fill:#f3e5f5,stroke:#8e24aa,color:#4a148c;
    classDef ttl300 fill:#ffebee,stroke:#e53935,color:#b71c1c;
```

**Padrão:** leitura tenta cache primeiro; mutações invalidam chaves pontuais e/ou por `DelPattern`.

---

## 11) Segurança e Controles

```mermaid
flowchart LR
    A[AuthMiddleware] -->|JWT Bearer| B[user_id/user_uuid/username em Locals]
    C[OptionalAuthMiddleware] --> D[Rotas públicas com contexto opcional]
    E[AdminMiddleware] -->|X-Admin-Key + constant time compare| F[Rotas administrativas]
    G[Rate limiter] --> H[/auth/register,/auth/login,/auth/forgot-password,/auth/reset-password]
    I[Cookie refresh_token] --> J[HttpOnly + SameSite Lax + Secure em produção]
```

- Tokens de sessão persistidos em `sessions` como **hash SHA-256** (não texto puro).
- Senhas com `bcrypt`.
- OAuth com `oauth_state` para proteção CSRF.
- Limpeza periódica de `sessions` e `password_reset_tokens` expirados.

---

## 12) Infra e Deploy

```mermaid
flowchart LR
    SRC[Código Go] --> BUILD[build.sh / go build]
    BUILD --> BIN[binário portal]
    SRC --> DOCKER[Docker multi-stage]
    DOCKER --> IMG[Imagem Alpine + /portal]
    IMG --> RUN[Container :8082]
```

**Stack principal:** Fiber v2, PostgreSQL (`lib/pq`), Redis (`go-redis/v9`), JWT v5, OAuth2 Google, Resend/SMTP, Protobuf.

---

## 13) Variáveis de Ambiente (catálogo)

```mermaid
mindmap
  root((ENV))
    Core
      PORT
      DATABASE_URL
      REDIS_URL
      JWT_SECRET
      ADMIN_SECRET_KEY
      GO_ENV
      FRONTEND_URL
      APP_NAME
    OAuth Google
      GOOGLE_CLIENT_ID
      GOOGLE_CLIENT_SECRET
      GOOGLE_REDIRECT_URL
    Email SMTP
      EMAIL_PROVIDER=smtp
      SMTP_HOST
      SMTP_PORT
      SMTP_USERNAME
      SMTP_PASSWORD
      SMTP_FROM
    Email Resend
      EMAIL_PROVIDER=resend
      RESEND_API_KEY
      RESEND_FROM
    Galeria
      CLOUDINARY_CLOUD_NAME
      CLOUDINARY_API_KEY
      CLOUDINARY_API_SECRET
```

> Recomendação operacional: manter segredos fora de repositório e rotacionar credenciais periodicamente.

---

## 14) Observações Técnicas Relevantes

- O handler `CreateRepost` existe em `pkg/handlers/social.go`, porém a rota HTTP de repost não está registrada atualmente em `cmd/server/main.go`.
- `GET /notifications` já marca notificações como lidas em background (`go MarkAsRead`).
- O sistema usa dois canais de atualização para front:
  - Pull via REST + cache Redis.
  - Push via WebSocket Hub com eventos broadcast.

---

## 15) Estrutura de Pastas (alto nível)

```mermaid
flowchart TB
    ROOT[backend-go-portal]
    ROOT --> CMD[cmd/server]
    ROOT --> PKG[pkg]
    ROOT --> PROTO[proto]

    PKG --> H[handlers]
    PKG --> S[services]
    PKG --> R[repository]
    PKG --> M[models]
    PKG --> MW[middleware]
    PKG --> C[cache]
    PKG --> D[database]
    PKG --> HUB[hub]
    PKG --> ENV[envelope]
    PKG --> SRV[server]
```

---

## 16) Execução local rápida

```bash
go mod download
go run ./cmd/server
```

ou com Docker:

```bash
docker build -t portal-backend .
docker run --env-file .env -p 8082:8082 portal-backend
```

---

## 17) Roadmap de documentação (próximos passos)

```mermaid
flowchart LR
    A[README Arquitetural] --> B[OpenAPI/Swagger]
    B --> C[Runbooks operacionais]
    C --> D[Playbooks de incidentes]
    D --> E[Dashboards de observabilidade]
```

Este README representa o estado implementado no código atual e pode ser usado como base de onboarding técnico, desenho de integração frontend e operação em produção.
