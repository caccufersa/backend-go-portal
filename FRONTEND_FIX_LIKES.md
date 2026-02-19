# Correção do Frontend: Likes Infinitos

Implementamos uma validação rígida no backend para impedir que um usuário curta o mesmo post múltiplas vezes ("likes infinitos"). O backend agora verifica a tabela `post_likes` antes de incrementar o contador.

Para que o Frontend funcione corretamente com essa nova lógica, siga as instruções abaixo:

## 1. Atualizações no Modelo de Dados

Os objetos `Post` retornados pelo backend agora incluem um novo campo booleano `liked`.

```typescript
interface Post {
  id: number;
  texto: string;
  author: string;
  user_id: number; // ID do autor do post
  likes: number;   // Contagem total
  liked: boolean;  // [NOVO] Se o usuário logado JÁ curtiu este post
  reply_count: number;
  created_at: string;
  // ...
}
```

## 2. Renderização do Botão de Like

Use o campo `liked` para definir o estado visual do botão.

- **Se `post.liked === true`**: Mostre o botão "Preenchido" / Ativo.
- **Se `post.liked === false`**: Mostre o botão "Vazio" / Inativo.

## 3. Lógica de Clique (Optimistic Update)

Ao clicar no botão de like, você deve:

1.  **Verificar o estado atual**: Se já estiver `liked`, a ação será `unlike`. Se não, `like`.
2.  **Atualização Otimista (Opcional mas recomendado)**:
    *   Inverta o `post.liked` visualmente.
    *   Incremente/Decremente `post.likes` visualmente (+1 ou -1) *imediatamente* para feedback instantâneo.
3.  **Enviar Requisição**: Chame `social.post.like` ou `social.post.unlike`.
4.  **Sincronizar com Resposta**:
    *   O backend retornará o objeto atualizado: `{ "post_id": 123, "likes": 45 }`.
    *   **Importante:** Se você enviou um like, mas o backend não incrementou (ex: o usuário já tinha curtido em outra aba), o backend retornará a contagem correta. Atualize sua UI com o valor retornado.

## 4. Tratamento de Erros e Consistência

O backend agora é idempotente para cada usuário.

- Enviar `like` num post que você já curtiu **não altera** a contagem e retorna 200 OK com a contagem atual.
- Enviar `unlike` num post que você não curtiu **não altera** a contagem e retorna 200 OK.

Isso previne o bug dos "clicks rápidos" gerando contagens erradas. O frontend pode ficar tranquilo em enviar requisições repetidas; o banco garantirá a consistência.
