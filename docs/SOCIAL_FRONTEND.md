# Documentação da API - Integração FrontEnd Social Feed

Este documento descreve detalhadamente como o FrontEnd (React/Vue/Angular) deve integrar e consumir a API do "Social Feed" do CACC. O módulo permite funcionalidades típicas de fórum: publicar tópicos, listar publicações, visualizar ramificações (respostas recursivas a uma postagem original), dar like, apagar postagens que o usuário for dono e atualizar o próprio perfil com nome de exibição e biografia estendidos.

---

## 🏗 Estruturas TypeScript

Estes são os modelos e tipagens que você deve utilizar nos arquivos do FrontEnd quando buscar ou salvar postagens baseadas em nossa interface Golang.

### Interface base `Post`

```typescript
export interface Post {
  id: number;
  texto: string;
  author: string;          // O username (unique handle) daquele usuário. Pense em algo como '@jose'.
  author_name: string;     // Nome principal que deve ser renderizado. Vem do 'display_name' customizado. Pense 'José Maria'.
  user_id: number;         // O ID da Auth que gerou a conta
  parent_id?: number;      // Campo opcional. Se a requisição retornou isso, então este objeto POST é um comentário (Reply) de outra thread.
  repost_id?: number;      // Se for um Repost, esse será o ID do post original que foi "Retweetado".
  avatar_url: string;      // A imagem hospedada no Cloudinary do usuário (retornada na listagem ou vazio caso não tenha).
  likes: number;           // Total de corações acumulados.
  reply_count: number;     // Total de respostas na thread.
  created_at: string;      // Timestamp ISO-8601 (Ex: 2026-03-22T19:00:20Z)
  liked: boolean;          // Boleano true/false informando se quem chamou a API já deu like na postagem atual.
  replies?: Post[];        // Nas rotas de Feed ou Thread, aqui virá a árvore populada de respostas.
}
```

### Interface base `Profile`

```typescript
export interface Profile {
  username: string;       // O username @handle da pessoa que é a dona do perfil
  display_name: string;   // O nome social escolhido e salvo pela pessoa em perfil
  bio: string;            // A minibiografia customizada escrita pela pessoa
  avatar_url: string;     // URL da foto hospedada externamente.
  total_posts: number;    // Acumulado do histórico quantitativo de posts dele
  total_likes: number;    // Quantidade mágica que a pessoa obteve nas postagens somadas
  posts: Post[];          // Um subarray das próprias postagens.
}
```

---

## 📡 Rotas REST e Regras de Negócio

Lembre-se: Todas as rotas baseadas no social exigirão da sua aplicação o Bearer token se for de edição, e retornarão a flag `liked: true` se repassar o token autenticando num endpoint publico.

### 1. 📂 O Feed e Tópicos (Públicas - Leitura)

**Endpoint:** `GET /social/feed`
Retorna as postagens de nível alto (as que começaram conversas originais, ou seja, onde `parent_id` é nulo).
- **Queries disponiveis:** `?limit=30` (A cada página quantos buscar, padrão é 30), `?offset=0` (Quantos pular na paginação, padrão 0).

**Endpoint:** `GET /social/feed/:id`
Carrega especificamente os detalhes gigantescos e profundos da árvore recursiva de comentários atrelados a um post especifico. 
- **Parâmetros Param:** `id` -> Substitua pelor valor do inteiro do ID da publicação (ex: `/social/feed/4010`). O Payload que será retornado é estritamente um item Unitário daquela sua interface TypeScript `Post` (E na key "replies" estará recursivamente todo o conteúdo interno daquele comentário caso haja!).

### 2. 🪪 Perfis & Configurações de Aparência (Profile)

**Endpoint:** `GET /social/profile` 
Traz o Perfil (`Profile`) de QUEM ESTÁ AUTENTICADO.

**Endpoint:** `GET /social/profile/:username`
Mesma função a de cima, porém ele vai carregar um Perfil de Terceiros ao tentar achar via URL. (Ex: `/social/profile/jose_silva12`). Se ele não encontrar ele retornará um `404 Not Found`.

**Endpoint de edição:** `PUT /social/profile` -> *(Necessita HTTP Bearer Autorization)*
Sempre que você desenvolver o Modal do UI, monte a submissão com esse pequeno JSON body para alterar os detalhes de personalização do User logado:

**Body Model:**
```json
{
  "display_name": "Zé Dev Front",
  "bio": "Estudo React a vida inteira e sigo em pé!",
  "avatar_url": "https://meu_host_cloudinary.com/foto.png"
}
```
**Regras do PUT:** O `display_name` tem limite máximo de tamanho de 50 caracteres. O campo de bio aceita até 500 no Banco. `avatar_url` é esperado após uplaod na dashboard de conta.

### 3. ✍ Postar, Responder e Deletar conteúdos (Interações Mutáveis Exigem Auth)

**Postar Tópico:** `POST /social/feed` // Body requerido: `{"texto": "Conteúdo com menção a @joao"}` (O Backend automaticamente localiza os @ e gera a notificação pra eles).
**Responder a um Post (Comentário):** `POST /social/feed/:id/reply` // Body requerido: `{"texto": "Sua resposta..."}`. Onde o :id na URL é do Post pai a qual este usuário clicar em responder.
- Respostas criativas retornarão com sucesso um 201 contendo o `Post` cru inserido. Se você estiver num Socket UI, ela transbordará em tempo real.

**Repostar (Share):** `POST /social/feed/:id/repost` (Sem body). Reposta uma thread no mural de quem repassou, informando que a Action original não era sua.

**Dar Coração (Like):** `PUT /social/feed/:id/like` (Seja otimista no front, faça a transição de clique no coração mudar a UI da bool e dispare o PUT vazio pra API computar num job background assíncrono pro banco). 
- Ele Retornará uma reposta de controle de integridade `{"post_id": 1500, "likes": 2}`.

**Remover Coração (Unlike):** `DELETE /social/feed/:id/like` (Análogo ao de cima).

**Deletar um Post Original Dele:** `DELETE /social/feed/:id` 
- Apaga e rebaixa as dependências atreladas, como comentários.

---

## 🔔 Notificações In-App (Sininho)

Para exibir contadores e a bandeja de Interações de outros usuários usando o fórum com o usuário logado:

**Listar Notificações:** `GET /notifications` (Retorna array com `?limit=20` por padrão. Também autometicamente aciona um background-job local alterando todas pra "read = true" depois da leitura).

**Retorno do Array de Objetos:**
```typescript
interface Notification {
  id: number;
  user_id: number;
  actor_id: number;           // Quem gerou a ação (Ex: ID do joão que curtiu seu post)
  actor_name: string;         // Nome do João pra renderizar (Ex: "João Dev")
  actor_avatar: string;       // Avatar pra UI bolinha do dropdown
  type: "like" | "reply" | "mention" | "repost"; 
  post_id: number;            // Qual post ele mexeu (Linkável pelo front até o /feed/id)
  is_read: boolean;           // Boleano visual, se a cor deve estar acesa nas n lidas.
  created_at: string;
}
```

**Zerar Avisos Manualmente:** `PUT /notifications/read` // Passa todas as Unread pra read sem retornar payloads gigantescos.

---

## ⚡ WebSockets Integrados em Alta Voltagem e Sync 

Se o Frontend se conectar via Socket no endereço original `wss://URL_AQUI/ws?token=Bearer...`, e você desenvolver os hooks necessários, seu client escutará e propagará todas as edições sem dar Reload na View (Single Page Application fluída). As instâncias emitirão Broadcast pela key base global `event.channel === "social"`.

Tratamentos recomendados no WebSockets em Switch Cases ou dispatchers de stores visuais:

*   **Açao: `new_post`**: A mensagem de Payload já é um json perfeitamente adaptado na interface de post `Post`. Junte num array `unshift` de postagens para fazer aparecer no Top Feed do Fórum.
*   **Ação: `new_reply`**: O Payload conterá `{"reply": Post, "parent_id": 120}`. Significa que, se ele encontrar um React Node renderizado da caixinha post 120 e estiver "aberta", ele simplesmente emenda essa mensagem ali em baixo sem o reload da tela!.
*   **Ação: `post_liked`**: O payload terá `{"likes": NOVO_TOTAL_ARITMETICO, "post_id": 120}`. Utilize o mapping findById na sua estrutura global do fórum e simplesmente troque o `.likes = valor_novo`.
*   **Açao: `post_deleted`**: Limpe com `filter()` se achar o id no Client da view, com o body sendo `{"post_id": ID_REMOVIDO_DA_VEZ}`. 
*   **Ação: `profile_updated`**: Refresco em massa! Troque de forma retroativa os arrays do Fórum caso hajam mensagens dele com o payload `{"display_name": "Meu Novo Nome Famoso", "user_id": 999}`.
