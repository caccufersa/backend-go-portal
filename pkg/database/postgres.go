package database

import (
	"database/sql"
	"log"
	"os"

	_ "github.com/lib/pq"
)

func Connect() *sql.DB {
	connStr := os.Getenv("DATABASE_URL")
	if connStr == "" {
		log.Println("Aviso: DATABASE_URL não definida.")
	}

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal("Erro ao abrir conexão:", err)
	}

	if err = db.Ping(); err != nil {
		log.Fatal("Erro Ping Banco:", err)
	}

	// Configuração do Pool de Conexões
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * 60 * 1000000000) // 5 minutos

	initDB(db)

	log.Println("Conexão com PostgreSQL estabelecida.")
	return db
}

func initDB(db *sql.DB) {
	schema := `
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE IF NOT EXISTS users (
	id SERIAL PRIMARY KEY,
	uuid UUID UNIQUE NOT NULL DEFAULT gen_random_uuid(),
	username TEXT UNIQUE NOT NULL,
	password TEXT NOT NULL,
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS sessions (
	id SERIAL PRIMARY KEY,
	user_id INT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	refresh_token TEXT UNIQUE NOT NULL,
	user_agent TEXT NOT NULL DEFAULT '',
	ip TEXT NOT NULL DEFAULT '',
	expires_at TIMESTAMP NOT NULL,
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS posts (
	id SERIAL PRIMARY KEY,
	texto TEXT NOT NULL,
	author TEXT NOT NULL DEFAULT 'Anônimo',
	user_id INT REFERENCES users(id) ON DELETE SET NULL,
	parent_id INT REFERENCES posts(id) ON DELETE CASCADE,
	likes INT NOT NULL DEFAULT 0,
	reply_count INT NOT NULL DEFAULT 0,
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS noticias (
	id SERIAL PRIMARY KEY,
	titulo TEXT NOT NULL,
	conteudo TEXT NOT NULL,
	resumo TEXT NOT NULL DEFAULT '',
	author TEXT NOT NULL DEFAULT 'Anônimo',
	categoria TEXT NOT NULL DEFAULT 'Geral',
	image_url TEXT,
	destaque BOOLEAN NOT NULL DEFAULT false,
	tags TEXT[] DEFAULT '{}',
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS sugestoes (
	id SERIAL PRIMARY KEY,
	texto TEXT NOT NULL,
	data_criacao TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	author TEXT DEFAULT 'Anônimo',
	categoria TEXT DEFAULT 'Geral'
);

CREATE TABLE IF NOT EXISTS post_likes (
	user_id INT NOT NULL,
	post_id INT NOT NULL,
	created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	PRIMARY KEY (user_id, post_id)
);

CREATE TABLE IF NOT EXISTS social_profiles (
	user_id INT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
	display_name TEXT,
	bio TEXT,
	created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS bus_seats (
	trip_id TEXT NOT NULL,
	seat_number INT NOT NULL,
	user_id INT REFERENCES users(id) ON DELETE SET NULL,
	reserved_at TIMESTAMP,
	PRIMARY KEY (trip_id, seat_number)
);

ALTER TABLE users ADD COLUMN IF NOT EXISTS uuid UUID UNIQUE DEFAULT gen_random_uuid();
UPDATE users SET uuid = gen_random_uuid() WHERE uuid IS NULL;
ALTER TABLE posts ADD COLUMN IF NOT EXISTS user_id INT REFERENCES users(id) ON DELETE SET NULL;
ALTER TABLE posts ADD COLUMN IF NOT EXISTS reply_count INT NOT NULL DEFAULT 0;
UPDATE posts p SET reply_count = (SELECT COUNT(*) FROM posts c WHERE c.parent_id = p.id) WHERE reply_count = 0;
ALTER TABLE noticias ADD COLUMN IF NOT EXISTS tags TEXT[] DEFAULT '{}';
ALTER TABLE sugestoes ADD COLUMN IF NOT EXISTS author TEXT;
ALTER TABLE sugestoes ADD COLUMN IF NOT EXISTS categoria TEXT DEFAULT 'Geral';

INSERT INTO bus_seats (trip_id, seat_number) 
 SELECT 't1', generate_series(1, 36)
 WHERE NOT EXISTS (SELECT 1 FROM bus_seats WHERE trip_id = 't1');

INSERT INTO bus_seats (trip_id, seat_number) 
 SELECT 't2', generate_series(1, 44)
 WHERE NOT EXISTS (SELECT 1 FROM bus_seats WHERE trip_id = 't2');

CREATE INDEX IF NOT EXISTS idx_users_username ON users(username);
CREATE INDEX IF NOT EXISTS idx_users_uuid ON users(uuid);
CREATE INDEX IF NOT EXISTS idx_sessions_token ON sessions(refresh_token);
CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at);
CREATE INDEX IF NOT EXISTS idx_posts_parent ON posts(parent_id);
CREATE INDEX IF NOT EXISTS idx_posts_created ON posts(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_posts_likes ON posts(likes DESC);
CREATE INDEX IF NOT EXISTS idx_posts_author ON posts(author);
CREATE INDEX IF NOT EXISTS idx_posts_user_id ON posts(user_id);
CREATE INDEX IF NOT EXISTS idx_posts_feed ON posts(created_at DESC) WHERE parent_id IS NULL;
CREATE INDEX IF NOT EXISTS idx_posts_user_created ON posts(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_posts_parent_created ON posts(parent_id, created_at ASC);
CREATE INDEX IF NOT EXISTS idx_posts_reply_count ON posts(reply_count) WHERE reply_count > 0;
CREATE INDEX IF NOT EXISTS idx_noticias_created ON noticias(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_noticias_categoria ON noticias(categoria);
CREATE INDEX IF NOT EXISTS idx_noticias_destaque ON noticias(destaque) WHERE destaque = true;
CREATE INDEX IF NOT EXISTS idx_noticias_tags ON noticias USING GIN(tags);
CREATE INDEX IF NOT EXISTS idx_post_likes_user ON post_likes(user_id);
CREATE INDEX IF NOT EXISTS idx_post_likes_post ON post_likes(post_id);

CREATE TABLE IF NOT EXISTS bus_trips (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	description TEXT,
	departure_time TIMESTAMP,
	total_seats INT NOT NULL,
	is_completed BOOLEAN NOT NULL DEFAULT false,
	created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
	updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO bus_trips (id, name, description, total_seats) 
VALUES ('t1', 'Viagem Padrão 1', 'Descrição da viagem 1', 36)
ON CONFLICT DO NOTHING;

INSERT INTO bus_trips (id, name, description, total_seats) 
VALUES ('t2', 'Viagem Padrão 2', 'Descrição da viagem 2', 44)
ON CONFLICT DO NOTHING;
`
	_, err := db.Exec(schema)
	if err != nil {
		log.Printf("[DB] Erro ao inicializar schema básico: %v", err)
	}

	fkSchema := `
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_bus_seats_trip') THEN
        ALTER TABLE bus_seats ADD CONSTRAINT fk_bus_seats_trip FOREIGN KEY (trip_id) REFERENCES bus_trips(id) ON DELETE CASCADE;
    END IF;
END $$;
`
	_, err = db.Exec(fkSchema)
	if err != nil {
		log.Printf("[DB] Erro ao inicializar a foreign key de bus_seats: %v", err)
	}

	log.Println("[DB] Inline schema initialization completed.")
}
