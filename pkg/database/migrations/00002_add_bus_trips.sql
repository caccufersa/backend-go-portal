-- +goose Up
-- +goose StatementBegin

-- Create Trips Table
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

-- Seed existing trips so foreign keys don't break
INSERT INTO bus_trips (id, name, description, total_seats) 
VALUES ('t1', 'Viagem Padrão 1', 'Descrição da viagem 1', 36)
ON CONFLICT DO NOTHING;

INSERT INTO bus_trips (id, name, description, total_seats) 
VALUES ('t2', 'Viagem Padrão 2', 'Descrição da viagem 2', 44)
ON CONFLICT DO NOTHING;

-- Add a foreign key constraint linking bus_seats to bus_trips
-- Since trip_id is already a TEXT, we can just alter it.
-- But we need to make sure the types match and rows exist.
ALTER TABLE bus_seats
ADD CONSTRAINT fk_bus_seats_trip
FOREIGN KEY (trip_id) REFERENCES bus_trips(id) ON DELETE CASCADE;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE bus_seats DROP CONSTRAINT IF EXISTS fk_bus_seats_trip;
DROP TABLE IF EXISTS bus_trips CASCADE;
-- +goose StatementEnd
