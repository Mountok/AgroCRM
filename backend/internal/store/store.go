package store

import (
	"context"
	"database/sql"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"golang.org/x/crypto/bcrypt"
)

type Store struct{ DB *sql.DB }

func Open(dsn string) (*Store, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	return &Store{DB: db}, nil
}

func (s *Store) Wait(ctx context.Context) error {
	for {
		if err := s.DB.PingContext(ctx); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func (s *Store) Migrate() error {
	_, err := s.DB.Exec(schemaSQL)
	return err
}

func (s *Store) Seed() error {
	var farmID, cropID int
	if err := s.DB.QueryRow("SELECT id FROM farms ORDER BY id LIMIT 1").Scan(&farmID); err == sql.ErrNoRows {
		if err := s.DB.QueryRow("INSERT INTO farms(name, region, address) VALUES($1,$2,$3) RETURNING id", "Ферма Ахмат", "Чеченская Республика", "Грозненский район").Scan(&farmID); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	hash, _ := bcrypt.GenerateFromPassword([]byte("demo123"), bcrypt.DefaultCost)
	if _, err := s.DB.Exec(`INSERT INTO users(farm_id,name,email,phone,password_hash,role)
VALUES($1,$2,$3,$4,$5,'owner')
ON CONFLICT (email) DO UPDATE SET farm_id=EXCLUDED.farm_id, name=EXCLUDED.name, phone=EXCLUDED.phone, password_hash=EXCLUDED.password_hash, role='owner'`, farmID, "Ахмат Исаев", "akhmat@example.com", "+7 (928) 123-45-67", string(hash)); err != nil {
		return err
	}
	adminHash, _ := bcrypt.GenerateFromPassword([]byte("admin"), bcrypt.DefaultCost)
	if _, err := s.DB.Exec(`INSERT INTO users(farm_id,name,email,phone,password_hash,role)
VALUES($1,$2,$3,$4,$5,'admin')
ON CONFLICT (email) DO UPDATE SET farm_id=EXCLUDED.farm_id, name=EXCLUDED.name, phone=EXCLUDED.phone, password_hash=EXCLUDED.password_hash, role='admin'`, farmID, "Администратор AgroCRM", "admin@agrocrm.local", "", string(adminHash)); err != nil {
		return err
	}
	_, _ = s.DB.Exec(`INSERT INTO fields(farm_id,name,area_hectares,location,soil_type,status)
SELECT $1,$2,$3,$4,$5,'ready' WHERE NOT EXISTS (SELECT 1 FROM fields WHERE farm_id=$1 AND name=$2)`, farmID, "Поле №1", 2, "Грозненский район", "чернозём")
	if err := s.DB.QueryRow("SELECT id FROM crops WHERE farm_id=$1 AND name=$2 LIMIT 1", farmID, "Картофель").Scan(&cropID); err == sql.ErrNoRows {
		if err := s.DB.QueryRow("INSERT INTO crops(farm_id,name,category,default_seed_rate_kg_per_hectare,default_price_per_kg) VALUES($1,$2,$3,$4,$5) RETURNING id", farmID, "Картофель", "Овощи", 250, 35).Scan(&cropID); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	_, _ = s.DB.Exec(`INSERT INTO inventory_items(farm_id,type,name,crop_id,quantity_kg,min_quantity_kg,average_cost_per_kg)
SELECT $1,'seed',$2,$3,700,300,18 WHERE NOT EXISTS (SELECT 1 FROM inventory_items WHERE farm_id=$1 AND crop_id=$3 AND type='seed')`, farmID, "Семена картофеля", cropID)
	_, _ = s.DB.Exec(`INSERT INTO customers(farm_id,name,phone,email,type)
SELECT $1,$2,$3,$4,'restaurant' WHERE NOT EXISTS (SELECT 1 FROM customers WHERE farm_id=$1 AND name=$2)`, farmID, "Ресторан Кавказ", "+7 900 000 00 00", "buyer@example.com")
	return nil
}

func (s *Store) Rows(query string, args ...any) ([]map[string]any, error) {
	rows, err := s.DB.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	out := []map[string]any{}
	for rows.Next() {
		vals := make([]any, len(cols))
		ptr := make([]any, len(cols))
		for i := range vals {
			ptr[i] = &vals[i]
		}
		if err := rows.Scan(ptr...); err != nil {
			return nil, err
		}
		item := map[string]any{}
		for i, col := range cols {
			item[col] = vals[i]
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) One(query string, args ...any) (map[string]any, bool, error) {
	items, err := s.Rows(query, args...)
	if err != nil || len(items) == 0 {
		return nil, false, err
	}
	return items[0], true, nil
}

const schemaSQL = `
CREATE TABLE IF NOT EXISTS farms(id SERIAL PRIMARY KEY, name TEXT NOT NULL, region TEXT DEFAULT '', address TEXT DEFAULT '', created_at TIMESTAMP DEFAULT now(), updated_at TIMESTAMP DEFAULT now());
ALTER TABLE farms ADD COLUMN IF NOT EXISTS address TEXT DEFAULT '';
ALTER TABLE farms ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP DEFAULT now();
CREATE TABLE IF NOT EXISTS users(id SERIAL PRIMARY KEY, farm_id INT REFERENCES farms(id), name TEXT NOT NULL DEFAULT '', email TEXT UNIQUE NOT NULL, phone TEXT DEFAULT '', password_hash TEXT DEFAULT '', role TEXT DEFAULT 'owner', created_at TIMESTAMP DEFAULT now(), updated_at TIMESTAMP DEFAULT now());
ALTER TABLE users ADD COLUMN IF NOT EXISTS phone TEXT DEFAULT '';
ALTER TABLE users ADD COLUMN IF NOT EXISTS password_hash TEXT DEFAULT '';
ALTER TABLE users ADD COLUMN IF NOT EXISTS role TEXT DEFAULT 'owner';
ALTER TABLE users ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP DEFAULT now();
CREATE TABLE IF NOT EXISTS fields(id SERIAL PRIMARY KEY, farm_id INT REFERENCES farms(id), name TEXT NOT NULL, area_hectares NUMERIC DEFAULT 0, location TEXT DEFAULT '', soil_type TEXT DEFAULT '', status TEXT DEFAULT 'ready', created_at TIMESTAMP DEFAULT now(), updated_at TIMESTAMP DEFAULT now());
ALTER TABLE fields ADD COLUMN IF NOT EXISTS soil_type TEXT DEFAULT '';
ALTER TABLE fields ADD COLUMN IF NOT EXISTS created_at TIMESTAMP DEFAULT now();
ALTER TABLE fields ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP DEFAULT now();
CREATE TABLE IF NOT EXISTS crops(id SERIAL PRIMARY KEY, farm_id INT REFERENCES farms(id), name TEXT NOT NULL, category TEXT DEFAULT '', default_seed_rate_kg_per_hectare NUMERIC DEFAULT 0, default_price_per_kg NUMERIC DEFAULT 0, created_at TIMESTAMP DEFAULT now(), updated_at TIMESTAMP DEFAULT now());
ALTER TABLE crops ADD COLUMN IF NOT EXISTS category TEXT DEFAULT '';
ALTER TABLE crops ADD COLUMN IF NOT EXISTS default_seed_rate_kg_per_hectare NUMERIC DEFAULT 0;
ALTER TABLE crops ADD COLUMN IF NOT EXISTS created_at TIMESTAMP DEFAULT now();
ALTER TABLE crops ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP DEFAULT now();
CREATE TABLE IF NOT EXISTS inventory_items(id SERIAL PRIMARY KEY, farm_id INT REFERENCES farms(id), type TEXT NOT NULL, name TEXT NOT NULL, crop_id INT REFERENCES crops(id), quantity_kg NUMERIC DEFAULT 0, unit TEXT DEFAULT 'кг', min_quantity_kg NUMERIC DEFAULT 0, average_cost_per_kg NUMERIC DEFAULT 0, created_at TIMESTAMP DEFAULT now(), updated_at TIMESTAMP DEFAULT now());
ALTER TABLE inventory_items ADD COLUMN IF NOT EXISTS unit TEXT DEFAULT 'кг';
ALTER TABLE inventory_items ADD COLUMN IF NOT EXISTS created_at TIMESTAMP DEFAULT now();
ALTER TABLE inventory_items ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP DEFAULT now();
CREATE TABLE IF NOT EXISTS plantings(id SERIAL PRIMARY KEY, farm_id INT REFERENCES farms(id), field_id INT REFERENCES fields(id), crop_id INT REFERENCES crops(id), planting_date DATE DEFAULT current_date, planned_harvest_date DATE, seed_quantity_kg NUMERIC NOT NULL, expected_yield_kg NUMERIC DEFAULT 0, status TEXT DEFAULT 'active', cost_amount NUMERIC DEFAULT 0, created_at TIMESTAMP DEFAULT now(), updated_at TIMESTAMP DEFAULT now());
ALTER TABLE plantings ADD COLUMN IF NOT EXISTS planting_date DATE DEFAULT current_date;
ALTER TABLE plantings ADD COLUMN IF NOT EXISTS planned_harvest_date DATE;
ALTER TABLE plantings ADD COLUMN IF NOT EXISTS cost_amount NUMERIC DEFAULT 0;
ALTER TABLE plantings ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP DEFAULT now();
CREATE TABLE IF NOT EXISTS tasks(id SERIAL PRIMARY KEY, farm_id INT REFERENCES farms(id), field_id INT REFERENCES fields(id), planting_id INT REFERENCES plantings(id), title TEXT NOT NULL, description TEXT DEFAULT '', type TEXT DEFAULT 'planting', status TEXT DEFAULT 'todo', due_date DATE DEFAULT current_date, created_at TIMESTAMP DEFAULT now(), updated_at TIMESTAMP DEFAULT now());
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS field_id INT REFERENCES fields(id);
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS description TEXT DEFAULT '';
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS type TEXT DEFAULT 'planting';
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS created_at TIMESTAMP DEFAULT now();
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP DEFAULT now();
CREATE TABLE IF NOT EXISTS harvests(id SERIAL PRIMARY KEY, farm_id INT REFERENCES farms(id), planting_id INT REFERENCES plantings(id), field_id INT REFERENCES fields(id), crop_id INT REFERENCES crops(id), quantity_kg NUMERIC NOT NULL, harvest_date DATE DEFAULT current_date, quality_grade TEXT DEFAULT '', added_to_inventory_item_id INT REFERENCES inventory_items(id), created_at TIMESTAMP DEFAULT now());
ALTER TABLE harvests ADD COLUMN IF NOT EXISTS field_id INT REFERENCES fields(id);
ALTER TABLE harvests ADD COLUMN IF NOT EXISTS quality_grade TEXT DEFAULT '';
ALTER TABLE harvests ADD COLUMN IF NOT EXISTS added_to_inventory_item_id INT REFERENCES inventory_items(id);
CREATE TABLE IF NOT EXISTS customers(id SERIAL PRIMARY KEY, farm_id INT REFERENCES farms(id), name TEXT NOT NULL, type TEXT DEFAULT 'restaurant', phone TEXT DEFAULT '', email TEXT DEFAULT '', address TEXT DEFAULT '', notes TEXT DEFAULT '', created_at TIMESTAMP DEFAULT now(), updated_at TIMESTAMP DEFAULT now());
ALTER TABLE customers ADD COLUMN IF NOT EXISTS email TEXT DEFAULT '';
ALTER TABLE customers ADD COLUMN IF NOT EXISTS address TEXT DEFAULT '';
ALTER TABLE customers ADD COLUMN IF NOT EXISTS notes TEXT DEFAULT '';
ALTER TABLE customers ADD COLUMN IF NOT EXISTS created_at TIMESTAMP DEFAULT now();
ALTER TABLE customers ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP DEFAULT now();
CREATE TABLE IF NOT EXISTS sales(id SERIAL PRIMARY KEY, farm_id INT REFERENCES farms(id), customer_id INT REFERENCES customers(id), inventory_item_id INT REFERENCES inventory_items(id), crop_id INT REFERENCES crops(id), quantity_kg NUMERIC NOT NULL, price_per_kg NUMERIC NOT NULL, revenue_amount NUMERIC NOT NULL, cost_amount NUMERIC DEFAULT 0, profit_amount NUMERIC NOT NULL, sale_date DATE DEFAULT current_date, status TEXT DEFAULT 'paid', created_at TIMESTAMP DEFAULT now());
ALTER TABLE sales ADD COLUMN IF NOT EXISTS crop_id INT REFERENCES crops(id);
ALTER TABLE sales ADD COLUMN IF NOT EXISTS cost_amount NUMERIC DEFAULT 0;
ALTER TABLE sales ADD COLUMN IF NOT EXISTS status TEXT DEFAULT 'paid';
CREATE TABLE IF NOT EXISTS purchases(id SERIAL PRIMARY KEY, farm_id INT REFERENCES farms(id), inventory_item_id INT REFERENCES inventory_items(id), supplier_name TEXT DEFAULT '', quantity_kg NUMERIC NOT NULL, price_per_kg NUMERIC NOT NULL, total_amount NUMERIC NOT NULL, purchase_date DATE DEFAULT current_date, created_at TIMESTAMP DEFAULT now());
CREATE TABLE IF NOT EXISTS api_keys(id SERIAL PRIMARY KEY, farm_id INT REFERENCES farms(id), name TEXT NOT NULL, key_hash TEXT NOT NULL, prefix TEXT NOT NULL, status TEXT DEFAULT 'active', last_used_at TIMESTAMP, created_at TIMESTAMP DEFAULT now(), revoked_at TIMESTAMP);
CREATE TABLE IF NOT EXISTS external_orders(id SERIAL PRIMARY KEY, farm_id INT REFERENCES farms(id), customer_name TEXT NOT NULL, phone TEXT DEFAULT '', email TEXT DEFAULT '', source TEXT DEFAULT 'public_api', status TEXT DEFAULT 'new', items_json JSONB DEFAULT '[]', total_quantity_kg NUMERIC DEFAULT 0, estimated_amount NUMERIC DEFAULT 0, created_at TIMESTAMP DEFAULT now(), updated_at TIMESTAMP DEFAULT now());
ALTER TABLE external_orders ADD COLUMN IF NOT EXISTS email TEXT DEFAULT '';
ALTER TABLE external_orders ADD COLUMN IF NOT EXISTS source TEXT DEFAULT 'public_api';
ALTER TABLE external_orders ADD COLUMN IF NOT EXISTS total_quantity_kg NUMERIC DEFAULT 0;
ALTER TABLE external_orders ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP DEFAULT now();
CREATE TABLE IF NOT EXISTS access_applications(id SERIAL PRIMARY KEY, owner_name TEXT NOT NULL, farm_name TEXT DEFAULT '', email TEXT DEFAULT '', phone TEXT DEFAULT '', land_area TEXT DEFAULT '', business_scale TEXT DEFAULT '', region TEXT DEFAULT '', comment TEXT DEFAULT '', status TEXT DEFAULT 'new', created_at TIMESTAMP DEFAULT now());
ALTER TABLE access_applications ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP DEFAULT now();
`
