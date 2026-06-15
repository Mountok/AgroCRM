package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"net/smtp"
	"os"
	"strings"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	_ "github.com/jackc/pgx/v5/stdlib"
)

type app struct{ db *sql.DB }

func main() {
	dsn := env("DATABASE_URL", "postgres://agrocrm:agrocrm@localhost:5432/agrocrm?sslmode=disable")
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	for db.PingContext(ctx) != nil {
		time.Sleep(time.Second)
	}
	a := &app{db: db}
	if err := a.migrate(); err != nil {
		log.Fatal(err)
	}
	if err := a.seed(); err != nil {
		log.Fatal(err)
	}

	r := gin.Default()
	r.Use(cors.New(cors.Config{AllowOrigins: []string{env("FRONTEND_ORIGIN", "http://localhost:5173")}, AllowMethods: []string{"GET", "POST", "PATCH", "DELETE", "OPTIONS"}, AllowHeaders: []string{"Origin", "Content-Type", "Authorization"}}))
	r.GET("/health", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })
	r.POST("/auth/register", a.register)
	r.POST("/auth/login", a.login)
	r.GET("/farms", a.listFarms)
	r.POST("/farms", a.createFarm)
	r.GET("/farms/:farmId/fields", a.listFields)
	r.POST("/farms/:farmId/fields", a.createField)
	r.GET("/farms/:farmId/crops", a.listCrops)
	r.POST("/farms/:farmId/crops", a.createCrop)
	r.GET("/farms/:farmId/inventory", a.listInventory)
	r.POST("/farms/:farmId/inventory", a.createInventory)
	r.GET("/farms/:farmId/plantings", a.listPlantings)
	r.POST("/farms/:farmId/plantings", a.createPlanting)
	r.GET("/farms/:farmId/tasks", a.listTasks)
	r.POST("/tasks/:taskId/complete", a.completeTask)
	r.GET("/farms/:farmId/harvests", a.listHarvests)
	r.POST("/farms/:farmId/harvests", a.createHarvest)
	r.GET("/farms/:farmId/customers", a.listCustomers)
	r.POST("/farms/:farmId/customers", a.createCustomer)
	r.GET("/farms/:farmId/sales", a.listSales)
	r.POST("/farms/:farmId/sales", a.createSale)
	r.GET("/farms/:farmId/dashboard", a.dashboard)
	r.GET("/public/products", a.publicProducts)
	r.POST("/public/orders", a.publicOrder)
	r.POST("/public/leads", a.publicLead)
	r.POST("/public/applications", a.publicApplication)
	log.Fatal(r.Run(":" + env("PORT", "8080")))
}

func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func (a *app) migrate() error {
	_, err := a.db.Exec(`
CREATE TABLE IF NOT EXISTS farms(id SERIAL PRIMARY KEY, name TEXT NOT NULL, region TEXT DEFAULT '', created_at TIMESTAMP DEFAULT now());
CREATE TABLE IF NOT EXISTS users(id SERIAL PRIMARY KEY, farm_id INT REFERENCES farms(id), name TEXT, email TEXT UNIQUE, role TEXT DEFAULT 'owner', created_at TIMESTAMP DEFAULT now());
CREATE TABLE IF NOT EXISTS fields(id SERIAL PRIMARY KEY, farm_id INT REFERENCES farms(id), name TEXT NOT NULL, area_hectares NUMERIC DEFAULT 0, location TEXT DEFAULT '', status TEXT DEFAULT 'ready');
CREATE TABLE IF NOT EXISTS crops(id SERIAL PRIMARY KEY, farm_id INT REFERENCES farms(id), name TEXT NOT NULL, default_price_per_kg NUMERIC DEFAULT 0);
CREATE TABLE IF NOT EXISTS inventory_items(id SERIAL PRIMARY KEY, farm_id INT REFERENCES farms(id), type TEXT NOT NULL, name TEXT NOT NULL, crop_id INT REFERENCES crops(id), quantity_kg NUMERIC DEFAULT 0, min_quantity_kg NUMERIC DEFAULT 0, average_cost_per_kg NUMERIC DEFAULT 0);
CREATE TABLE IF NOT EXISTS plantings(id SERIAL PRIMARY KEY, farm_id INT REFERENCES farms(id), field_id INT REFERENCES fields(id), crop_id INT REFERENCES crops(id), seed_quantity_kg NUMERIC NOT NULL, expected_yield_kg NUMERIC DEFAULT 0, status TEXT DEFAULT 'active', created_at TIMESTAMP DEFAULT now());
CREATE TABLE IF NOT EXISTS tasks(id SERIAL PRIMARY KEY, farm_id INT REFERENCES farms(id), planting_id INT REFERENCES plantings(id), title TEXT NOT NULL, status TEXT DEFAULT 'todo', due_date DATE DEFAULT current_date);
CREATE TABLE IF NOT EXISTS harvests(id SERIAL PRIMARY KEY, farm_id INT REFERENCES farms(id), planting_id INT REFERENCES plantings(id), crop_id INT REFERENCES crops(id), quantity_kg NUMERIC NOT NULL, harvest_date DATE DEFAULT current_date);
CREATE TABLE IF NOT EXISTS customers(id SERIAL PRIMARY KEY, farm_id INT REFERENCES farms(id), name TEXT NOT NULL, phone TEXT DEFAULT '', type TEXT DEFAULT 'restaurant');
CREATE TABLE IF NOT EXISTS sales(id SERIAL PRIMARY KEY, farm_id INT REFERENCES farms(id), customer_id INT REFERENCES customers(id), inventory_item_id INT REFERENCES inventory_items(id), quantity_kg NUMERIC NOT NULL, price_per_kg NUMERIC NOT NULL, revenue_amount NUMERIC NOT NULL, profit_amount NUMERIC NOT NULL, sale_date DATE DEFAULT current_date);
CREATE TABLE IF NOT EXISTS external_orders(id SERIAL PRIMARY KEY, farm_id INT REFERENCES farms(id), customer_name TEXT NOT NULL, phone TEXT DEFAULT '', items_json JSONB DEFAULT '[]', status TEXT DEFAULT 'new', estimated_amount NUMERIC DEFAULT 0, created_at TIMESTAMP DEFAULT now());
CREATE TABLE IF NOT EXISTS access_applications(id SERIAL PRIMARY KEY, owner_name TEXT NOT NULL, farm_name TEXT DEFAULT '', email TEXT DEFAULT '', phone TEXT DEFAULT '', land_area TEXT DEFAULT '', business_scale TEXT DEFAULT '', region TEXT DEFAULT '', comment TEXT DEFAULT '', status TEXT DEFAULT 'new', created_at TIMESTAMP DEFAULT now());`)
	return err
}

func (a *app) seed() error {
	var n int
	_ = a.db.QueryRow("SELECT count(*) FROM farms").Scan(&n)
	if n > 0 {
		return nil
	}
	var farmID, cropID int
	a.db.QueryRow("INSERT INTO farms(name, region) VALUES($1,$2) RETURNING id", "Ферма Ахмат", "Чеченская Республика").Scan(&farmID)
	a.db.Exec("INSERT INTO fields(farm_id,name,area_hectares,location) VALUES($1,$2,$3,$4)", farmID, "Поле №1", 2, "Грозненский район")
	a.db.QueryRow("INSERT INTO crops(farm_id,name,default_price_per_kg) VALUES($1,$2,$3) RETURNING id", farmID, "Картофель", 35).Scan(&cropID)
	a.db.Exec("INSERT INTO inventory_items(farm_id,type,name,crop_id,quantity_kg,min_quantity_kg,average_cost_per_kg) VALUES($1,'seed',$2,$3,700,300,18)", farmID, "Семена картофеля", cropID)
	a.db.Exec("INSERT INTO customers(farm_id,name,phone,type) VALUES($1,$2,$3,'restaurant')", farmID, "Ресторан Кавказ", "+7 900 000 00 00")
	return nil
}

func (a *app) register(c *gin.Context) {
	var in struct{ Name, Email, FarmName string }
	c.BindJSON(&in)
	var id int
	a.db.QueryRow("INSERT INTO farms(name,region) VALUES($1,$2) RETURNING id", val(in.FarmName, "Новая ферма"), "Чеченская Республика").Scan(&id)
	a.db.Exec("INSERT INTO users(farm_id,name,email) VALUES($1,$2,$3)", id, in.Name, in.Email)
	c.JSON(201, gin.H{"farmId": id})
}
func (a *app) login(c *gin.Context) {
	var in struct{ Email, Password string }
	c.BindJSON(&in)
	if in.Email == "" || in.Password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Введите логин и пароль"})
		return
	}
	var farmID int
	if err := a.db.QueryRow("SELECT id FROM farms ORDER BY id LIMIT 1").Scan(&farmID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, gin.H{"farmId": farmID, "name": "Ахмат Исаев", "role": "owner"})
}
func val(v, d string) string {
	if v == "" {
		return d
	}
	return v
}
func rows(c *gin.Context, db *sql.DB, q string, args ...any) {
	rs, err := db.Query(q, args...)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	defer rs.Close()
	cols, _ := rs.Columns()
	out := []map[string]any{}
	for rs.Next() {
		vals := make([]any, len(cols))
		ptr := make([]any, len(cols))
		for i := range vals {
			ptr[i] = &vals[i]
		}
		rs.Scan(ptr...)
		m := map[string]any{}
		for i, col := range cols {
			m[col] = vals[i]
		}
		out = append(out, m)
	}
	c.JSON(200, out)
}
func (a *app) listFarms(c *gin.Context) { rows(c, a.db, "SELECT * FROM farms ORDER BY id") }
func (a *app) createFarm(c *gin.Context) {
	var in struct{ Name, Region string }
	c.BindJSON(&in)
	var id int
	a.db.QueryRow("INSERT INTO farms(name,region) VALUES($1,$2) RETURNING id", in.Name, in.Region).Scan(&id)
	c.JSON(201, gin.H{"id": id})
}
func (a *app) listFields(c *gin.Context) {
	rows(c, a.db, "SELECT * FROM fields WHERE farm_id=$1 ORDER BY id", c.Param("farmId"))
}
func (a *app) createField(c *gin.Context) {
	var in struct {
		Name, Location string
		AreaHectares   float64 `json:"areaHectares"`
	}
	c.BindJSON(&in)
	a.db.Exec("INSERT INTO fields(farm_id,name,area_hectares,location) VALUES($1,$2,$3,$4)", c.Param("farmId"), in.Name, in.AreaHectares, in.Location)
	c.JSON(201, gin.H{"ok": true})
}
func (a *app) listCrops(c *gin.Context) {
	rows(c, a.db, "SELECT * FROM crops WHERE farm_id=$1 ORDER BY id", c.Param("farmId"))
}
func (a *app) createCrop(c *gin.Context) {
	var in struct {
		Name  string
		Price float64 `json:"pricePerKg"`
	}
	c.BindJSON(&in)
	a.db.Exec("INSERT INTO crops(farm_id,name,default_price_per_kg) VALUES($1,$2,$3)", c.Param("farmId"), in.Name, in.Price)
	c.JSON(201, gin.H{"ok": true})
}
func (a *app) listInventory(c *gin.Context) {
	rows(c, a.db, "SELECT * FROM inventory_items WHERE farm_id=$1 ORDER BY id", c.Param("farmId"))
}
func (a *app) createInventory(c *gin.Context) {
	var in struct {
		Type, Name                                  string
		CropID                                      int `json:"cropId"`
		QuantityKg, MinQuantityKg, AverageCostPerKg float64
	}
	c.BindJSON(&in)
	a.db.Exec("INSERT INTO inventory_items(farm_id,type,name,crop_id,quantity_kg,min_quantity_kg,average_cost_per_kg) VALUES($1,$2,$3,$4,$5,$6,$7)", c.Param("farmId"), in.Type, in.Name, in.CropID, in.QuantityKg, in.MinQuantityKg, in.AverageCostPerKg)
	c.JSON(201, gin.H{"ok": true})
}
func (a *app) listPlantings(c *gin.Context) {
	rows(c, a.db, "SELECT p.*, c.name crop_name, f.name field_name FROM plantings p JOIN crops c ON c.id=p.crop_id JOIN fields f ON f.id=p.field_id WHERE p.farm_id=$1 ORDER BY p.id DESC", c.Param("farmId"))
}
func (a *app) createPlanting(c *gin.Context) {
	var in struct {
		FieldID, CropID                 int
		SeedQuantityKg, ExpectedYieldKg float64
	}
	c.BindJSON(&in)
	tx, _ := a.db.Begin()
	defer tx.Rollback()
	var itemID int
	var qty, min float64
	err := tx.QueryRow("SELECT id,quantity_kg,min_quantity_kg FROM inventory_items WHERE farm_id=$1 AND crop_id=$2 AND type='seed' LIMIT 1", c.Param("farmId"), in.CropID).Scan(&itemID, &qty, &min)
	if err != nil || qty < in.SeedQuantityKg {
		c.JSON(409, gin.H{"error": "Недостаточно семян на складе"})
		return
	}
	tx.Exec("UPDATE inventory_items SET quantity_kg=quantity_kg-$1 WHERE id=$2", in.SeedQuantityKg, itemID)
	var pid int
	tx.QueryRow("INSERT INTO plantings(farm_id,field_id,crop_id,seed_quantity_kg,expected_yield_kg) VALUES($1,$2,$3,$4,$5) RETURNING id", c.Param("farmId"), in.FieldID, in.CropID, in.SeedQuantityKg, in.ExpectedYieldKg).Scan(&pid)
	for _, t := range []string{"Посадка картофеля", "Первый полив", "Обработка поля", "Проверка всходов"} {
		tx.Exec("INSERT INTO tasks(farm_id,planting_id,title,due_date) VALUES($1,$2,$3,current_date+3)", c.Param("farmId"), pid, t)
	}
	tx.Commit()
	c.JSON(201, gin.H{"id": pid, "remainingSeedKg": qty - in.SeedQuantityKg, "warning": qty-in.SeedQuantityKg < min})
}
func (a *app) listTasks(c *gin.Context) {
	rows(c, a.db, "SELECT * FROM tasks WHERE farm_id=$1 ORDER BY due_date,id", c.Param("farmId"))
}
func (a *app) completeTask(c *gin.Context) {
	a.db.Exec("UPDATE tasks SET status='done' WHERE id=$1", c.Param("taskId"))
	c.JSON(200, gin.H{"ok": true})
}
func (a *app) listHarvests(c *gin.Context) {
	rows(c, a.db, "SELECT h.*, c.name crop_name FROM harvests h JOIN crops c ON c.id=h.crop_id WHERE h.farm_id=$1 ORDER BY h.id DESC", c.Param("farmId"))
}
func (a *app) createHarvest(c *gin.Context) {
	var in struct {
		PlantingID, CropID int
		QuantityKg         float64
	}
	c.BindJSON(&in)
	tx, _ := a.db.Begin()
	tx.Exec("INSERT INTO harvests(farm_id,planting_id,crop_id,quantity_kg) VALUES($1,$2,$3,$4)", c.Param("farmId"), in.PlantingID, in.CropID, in.QuantityKg)
	var n int
	tx.QueryRow("SELECT count(*) FROM inventory_items WHERE farm_id=$1 AND crop_id=$2 AND type='harvest'", c.Param("farmId"), in.CropID).Scan(&n)
	if n == 0 {
		tx.Exec("INSERT INTO inventory_items(farm_id,type,name,crop_id,quantity_kg,min_quantity_kg,average_cost_per_kg) SELECT $1,'harvest',name,$2,$3,0,15 FROM crops WHERE id=$2", c.Param("farmId"), in.CropID, in.QuantityKg)
	} else {
		tx.Exec("UPDATE inventory_items SET quantity_kg=quantity_kg+$1 WHERE farm_id=$2 AND crop_id=$3 AND type='harvest'", in.QuantityKg, c.Param("farmId"), in.CropID)
	}
	tx.Commit()
	c.JSON(201, gin.H{"ok": true})
}
func (a *app) listCustomers(c *gin.Context) {
	rows(c, a.db, "SELECT * FROM customers WHERE farm_id=$1 ORDER BY id", c.Param("farmId"))
}
func (a *app) createCustomer(c *gin.Context) {
	var in struct{ Name, Phone, Type string }
	c.BindJSON(&in)
	a.db.Exec("INSERT INTO customers(farm_id,name,phone,type) VALUES($1,$2,$3,$4)", c.Param("farmId"), in.Name, in.Phone, val(in.Type, "restaurant"))
	c.JSON(201, gin.H{"ok": true})
}
func (a *app) listSales(c *gin.Context) {
	rows(c, a.db, "SELECT * FROM sales WHERE farm_id=$1 ORDER BY id DESC", c.Param("farmId"))
}
func (a *app) createSale(c *gin.Context) {
	var in struct {
		CustomerID, InventoryItemID int
		QuantityKg, PricePerKg      float64
	}
	c.BindJSON(&in)
	var qty, cost float64
	err := a.db.QueryRow("SELECT quantity_kg,average_cost_per_kg FROM inventory_items WHERE id=$1", in.InventoryItemID).Scan(&qty, &cost)
	if err != nil || qty < in.QuantityKg {
		c.JSON(409, gin.H{"error": "Недостаточно продукции на складе"})
		return
	}
	rev := in.QuantityKg * in.PricePerKg
	profit := rev - in.QuantityKg*cost
	a.db.Exec("UPDATE inventory_items SET quantity_kg=quantity_kg-$1 WHERE id=$2", in.QuantityKg, in.InventoryItemID)
	a.db.Exec("INSERT INTO sales(farm_id,customer_id,inventory_item_id,quantity_kg,price_per_kg,revenue_amount,profit_amount) VALUES($1,$2,$3,$4,$5,$6,$7)", c.Param("farmId"), in.CustomerID, in.InventoryItemID, in.QuantityKg, in.PricePerKg, rev, profit)
	c.JSON(201, gin.H{"revenue": rev, "profit": profit})
}
func (a *app) dashboard(c *gin.Context) {
	var revenue, profit float64
	a.db.QueryRow("SELECT COALESCE(sum(revenue_amount),0),COALESCE(sum(profit_amount),0) FROM sales WHERE farm_id=$1", c.Param("farmId")).Scan(&revenue, &profit)
	c.JSON(200, gin.H{"revenue": revenue, "profit": profit})
}
func (a *app) publicProducts(c *gin.Context) {
	rows(c, a.db, "SELECT i.id, i.name, i.quantity_kg as available_kg, c.default_price_per_kg as price_per_kg, f.name as farm_name FROM inventory_items i JOIN crops c ON c.id=i.crop_id JOIN farms f ON f.id=i.farm_id WHERE i.type='harvest' AND i.quantity_kg>0")
}
func (a *app) publicOrder(c *gin.Context) {
	var raw map[string]any
	c.BindJSON(&raw)
	name := fmt.Sprint(raw["customerName"])
	phone := fmt.Sprint(raw["phone"])
	a.db.Exec("INSERT INTO external_orders(farm_id,customer_name,phone,items_json) VALUES((SELECT id FROM farms ORDER BY id LIMIT 1),$1,$2,$3)", name, phone, raw["items"])
	c.JSON(http.StatusCreated, gin.H{"status": "new"})
}
func (a *app) publicLead(c *gin.Context) { a.publicOrder(c) }

func (a *app) publicApplication(c *gin.Context) {
	var in struct {
		OwnerName, FarmName, Email, Phone string
		LandArea, BusinessScale, Region   string
		Comment                           string
	}
	if err := c.BindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if strings.TrimSpace(in.OwnerName) == "" || strings.TrimSpace(in.Phone) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "ФИО и телефон обязательны"})
		return
	}
	var id int
	err := a.db.QueryRow(`INSERT INTO access_applications(owner_name,farm_name,email,phone,land_area,business_scale,region,comment) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`, in.OwnerName, in.FarmName, in.Email, in.Phone, in.LandArea, in.BusinessScale, in.Region, in.Comment).Scan(&id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := sendApplicationEmail(in, id); err != nil {
		log.Printf("application email skipped/failed: %v", err)
	}
	c.JSON(http.StatusCreated, gin.H{"id": id, "status": "new", "message": "Заявка принята. Мы свяжемся с вами и выдадим доступ."})
}

func sendApplicationEmail(in struct{ OwnerName, FarmName, Email, Phone, LandArea, BusinessScale, Region, Comment string }, id int) error {
	host := os.Getenv("SMTP_HOST")
	port := env("SMTP_PORT", "587")
	user := os.Getenv("SMTP_USER")
	pass := os.Getenv("SMTP_PASSWORD")
	to := env("APPLICATIONS_EMAIL", user)
	if host == "" || user == "" || pass == "" || to == "" {
		return fmt.Errorf("SMTP is not configured")
	}
	body := fmt.Sprintf("Новая заявка AgroCRM #%d\n\nФИО: %s\nФерма: %s\nТелефон: %s\nEmail: %s\nРегион: %s\nЗемля/масштаб: %s\nФормат хозяйства: %s\nКомментарий: %s\n", id, in.OwnerName, in.FarmName, in.Phone, in.Email, in.Region, in.LandArea, in.BusinessScale, in.Comment)
	msg := "To: " + to + "\r\nSubject: Новая заявка AgroCRM\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n" + body
	return smtp.SendMail(host+":"+port, smtp.PlainAuth("", user, pass, host), user, []string{to}, []byte(msg))
}
