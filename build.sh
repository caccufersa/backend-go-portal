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
  *)
    echo "Erro: SERVICE_NAME não definido ou inválido"
    echo "Use: sugestoes, social ou auth"
    exit 1
    ;;
esac
