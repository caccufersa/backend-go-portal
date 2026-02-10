#!/bin/bash

case "$SERVICE_NAME" in
  "sugestoes")
    echo "Building Sugestoes Service..."
    go build -o bin/sugestoes ./services/sugestoes
    ;;
  "social")
    echo "Building Social Service..."
    go build -o bin/social ./services/social
    ;;
  "auth")
    echo "Building Auth Service..."
    go build -o bin/auth ./services/auth
    ;;
  "noticias")
    echo "Building Noticias Service..."
    go build -o bin/noticias ./services/noticias
    ;;
  *)
    echo "Erro: SERVICE_NAME não definido ou inválido"
    echo "Use: sugestoes, social, auth ou noticias"
    exit 1
    ;;
esac
