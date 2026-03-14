# Plano de Migracao para VPS Caseira (Self-Hosted)

## 1. Objetivo

Migrar o backend para rodar 100% na VPS caseira, removendo dependencias de terceiros para:

- envio de e-mail transacional
- armazenamento de imagens da galeria

Meta final:

- API, Postgres, Redis, SMTP e galeria local rodando na VPS
- HTTPS valido em dominio proprio
- backup e rollback definidos

---

## 2. Escopo

### Incluido

- Deploy da API Go via Docker
- Banco Postgres via container oficial
- Redis via container oficial
- Reverse proxy com TLS automatico
- SMTP proprio na VPS para envio de e-mails
- Galeria em disco local da VPS
- Rotina de backup e observabilidade minima

### Fora de escopo (nesta fase)

- Alta disponibilidade multi-node
- CDN externa
- Painel de admin de e-mail

---

## 3. Resposta objetiva sobre banco de dados

Nao precisa criar Dockerfile para Postgres.

Use imagem oficial no docker compose. Dockerfile customizado para banco so e necessario se houver extensoes, tuning extremo ou scripts customizados de imagem.

---

## 4. Arquitetura alvo

- caddy/nginx (443/80) -> API Go (porta interna 8082)
- API Go -> Postgres (rede interna Docker)
- API Go -> Redis (rede interna Docker)
- API Go -> SMTP local (Postfix, porta 587 interna)
- API Go -> Storage local em volume montado (ex: /data/galeria)

Fluxo de galeria:

1. upload entra na API
2. API salva arquivo em disco local
3. API grava metadados no Postgres
4. API entrega URL publica de arquivo

Fluxo de e-mail:

1. API abre conexao SMTP local
2. Postfix entrega para destino externo
3. logs de entrega ficam na VPS

---

## 5. Pre-requisitos criticos (VPS caseira)

1. IP publico valido e estavel
2. Sem CGNAT (ou com port forwarding funcional)
3. Portas 80/443 liberadas para HTTPS
4. Porta 25 de saida liberada pelo provedor (muito importante para e-mail)
5. PTR/reverse DNS configuravel para o IP (necessario para entregabilidade)
6. DNS do dominio com controle total (A, MX, SPF, DKIM, DMARC)

Se a porta 25 de saida estiver bloqueada, envio direto de e-mail nao sera confiavel. Nesse caso, usar relay SMTP como fallback (mesmo mantendo API e storage locais).

---

## 6. Fases de execucao

## Fase 0 - Preparacao

1. Criar subdominios:
   - api.capcom.page
   - smtp.capcom.page
2. Ajustar DNS A para IP publico da VPS
3. Reduzir TTL para 300 antes da virada
4. Hardening basico da VPS:
   - usuario sem root
   - SSH por chave
   - firewall (22, 80, 443)

## Fase 1 - Infra base em containers

Servicos sugeridos no compose:

- api
- postgres
- redis
- caddy (ou nginx)
- postfix

Volumes persistentes:

- pg_data
- redis_data
- app_media (galeria)
- caddy_data (certificados)

## Fase 2 - Refatoracao de e-mail (sem terceiro)

Estado atual:

- O servico suporta Resend e SMTP

Objetivo:

- remover Resend do fluxo ativo
- padronizar SMTP local da VPS

Passos:

1. Definir EMAIL_PROVIDER=smtp em producao
2. Configurar SMTP_HOST=postfix
3. Configurar SMTP_PORT=587
4. Configurar SMTP_FROM com dominio proprio
5. Remover uso de RESEND_API_KEY/RESEND_FROM das configs de producao
6. (Opcional) remover dependencia resend-go do go.mod quando codigo for limpo

Checklist DNS de e-mail:

1. MX apontando para smtp.capcom.page
2. SPF incluindo host emissor
3. DKIM assinado pelo postfix/opendkim
4. DMARC com politica inicial p=none e depois endurecer
5. PTR alinhado com hostname SMTP

## Fase 3 - Refatoracao da galeria para storage local

Estado atual:

- Galeria depende de Cloudinary

Objetivo:

- armazenar imagens em disco local da VPS

Passos de arquitetura:

1. Criar interface de storage com metodos Upload/Delete
2. Criar implementacao LocalStorage:
   - salva em /data/galeria/YYYY/MM/uuid.ext
   - retorna URL publica (ex: https://api.capcom.page/media/...)
3. Manter metadados no Postgres (image_url, caminho, owner)
4. Expor rota publica para arquivos estaticos ou delegar ao proxy
5. Definir limite de tamanho e validacao de extensao MIME
6. Criar job de limpeza para arquivos orfaos

Variaveis novas sugeridas:

- STORAGE_PROVIDER=local
- STORAGE_ROOT=/data/galeria
- STORAGE_PUBLIC_BASE_URL=https://api.capcom.page/media
- MAX_UPLOAD_MB=10

## Fase 4 - Dados, virada e desativacao antiga // NAO PRECISA, SERVICO NAO ESTA EM PROD

1. Dump do banco atual
2. Restore no Postgres da VPS
3. Sincronizar imagens antigas (se houver)
4. Subir stack nova em paralelo
5. Validar endpoints criticos
6. Virar DNS para novo ambiente
7. Monitorar 24h a 48h
8. Desligar servicos antigos somente apos estabilidade

---

## 7. Exemplo de variaveis de ambiente (producao)

```env
GO_ENV=production
PORT=8082

JWT_SECRET=trocar_por_valor_forte
ADMIN_SECRET_KEY=trocar_por_valor_forte

DATABASE_URL=postgres://portal:senha_forte@postgres:5432/portal?sslmode=disable
REDIS_URL=redis://redis:6379

FRONTEND_URL=https://capcom.page
GOOGLE_CLIENT_ID=...
GOOGLE_CLIENT_SECRET=...
GOOGLE_REDIRECT_URL=https://api.capcom.page/auth/google/callback

EMAIL_PROVIDER=smtp
SMTP_HOST=postfix
SMTP_PORT=587
SMTP_USERNAME=
SMTP_PASSWORD=
SMTP_FROM=no-reply@capcom.page
APP_NAME=Portal CACC

STORAGE_PROVIDER=local
STORAGE_ROOT=/data/galeria
STORAGE_PUBLIC_BASE_URL=https://api.capcom.page/media
MAX_UPLOAD_MB=10
```

---

## 8. Seguranca minima recomendada

1. Nao expor Postgres e Redis para internet
2. Rodar tudo em rede Docker interna
3. TLS obrigatorio no dominio publico
4. Fail2ban/ufw no host
5. Backup criptografado diario fora da VPS
6. Rotacao de logs e monitor de disco

---

## 9. Backup e restore

## Banco

- backup diario com pg_dump
- retencao: 7 diarios, 4 semanais, 3 mensais

## Galeria

- snapshot diario de /data/galeria
- checksum de integridade

## Teste de restore

- executar restore completo em ambiente de homologacao 1x por mes

---

## 10. Criterios de aceite

1. /health responde 200 em https://api.capcom.page/health
2. Login, refresh e logout funcionando
3. Upload e delete da galeria funcionando com storage local
4. E-mail de reset/verificacao entregue sem provedor terceiro
5. Backup automatico validado
6. Nenhuma dependencia ativa de Cloudinary/Resend em producao

---

## 11. Riscos principais e mitigacao

1. Entregabilidade de e-mail baixa em IP residencial
   - Mitigar com PTR, SPF, DKIM, DMARC e reputacao gradual
2. Queda de energia/internet na casa
   - Mitigar com nobreak e monitoramento externo
3. Disco lotando por imagens
   - Mitigar com quotas, limpeza e alerta de uso
4. Bloqueio de porta 25 pelo ISP
   - Mitigar com relay SMTP como contingencia

---

## 12. Proximos entregaveis tecnicos

1. docker-compose.yml de producao
2. Caddyfile/Nginx com TLS
3. Refatoracao do servico de galeria para LocalStorage
4. Limpeza do servico de e-mail para SMTP-only
5. Script de backup (Postgres + media)
