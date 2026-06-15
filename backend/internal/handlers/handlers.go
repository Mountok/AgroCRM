package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"agrocrm/backend/internal/httpx"
	"agrocrm/backend/internal/store"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

type API struct {
	store   *store.Store
	limiter *httpx.Limiter
}

func New(store *store.Store, limiter *httpx.Limiter) *API {
	return &API{store: store, limiter: limiter}
}

func (a *API) RegisterRoutes(r *gin.Engine) {
	r.GET("/health", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })
	r.POST("/auth/register", a.limiter.Middleware(8, 15*time.Minute), a.register)
	r.POST("/auth/login", a.limiter.Middleware(12, 15*time.Minute), a.login)
	r.GET("/auth/me", func(c *gin.Context) {
		c.JSON(200, gin.H{"farmId": 1, "name": "Ахмат Исаев", "role": "owner"})
	})

	r.GET("/farms", a.list("SELECT id,name,region,address,created_at,updated_at FROM farms ORDER BY id"))
	r.POST("/farms", a.createFarm)
	r.GET("/farms/:farmId", a.one("SELECT * FROM farms WHERE id=$1", "farmId"))
	r.PATCH("/farms/:farmId", a.patchFarm)

	r.GET("/farms/:farmId/fields", a.listByFarm("SELECT * FROM fields WHERE farm_id=$1 ORDER BY id"))
	r.POST("/farms/:farmId/fields", a.createField)
	r.GET("/fields/:fieldId", a.one("SELECT * FROM fields WHERE id=$1", "fieldId"))
	r.PATCH("/fields/:fieldId", a.patchField)
	r.DELETE("/fields/:fieldId", a.delete("fields", "fieldId"))

	r.GET("/farms/:farmId/crops", a.listByFarm("SELECT * FROM crops WHERE farm_id=$1 ORDER BY id"))
	r.POST("/farms/:farmId/crops", a.createCrop)
	r.GET("/crops/:cropId", a.one("SELECT * FROM crops WHERE id=$1", "cropId"))
	r.PATCH("/crops/:cropId", a.patchCrop)
	r.DELETE("/crops/:cropId", a.delete("crops", "cropId"))

	r.GET("/farms/:farmId/inventory", a.listByFarm("SELECT * FROM inventory_items WHERE farm_id=$1 ORDER BY id"))
	r.POST("/farms/:farmId/inventory", a.createInventory)
	r.GET("/inventory/:itemId", a.one("SELECT * FROM inventory_items WHERE id=$1", "itemId"))
	r.PATCH("/inventory/:itemId", a.patchInventory)
	r.POST("/inventory/:itemId/adjust", a.adjustInventory)
	r.GET("/farms/:farmId/inventory/low-stock", a.listByFarm("SELECT * FROM inventory_items WHERE farm_id=$1 AND quantity_kg<=min_quantity_kg ORDER BY quantity_kg"))

	r.GET("/farms/:farmId/purchases", a.listByFarm("SELECT * FROM purchases WHERE farm_id=$1 ORDER BY id DESC"))
	r.POST("/farms/:farmId/purchases", a.createPurchase)
	r.GET("/purchases/:purchaseId", a.one("SELECT * FROM purchases WHERE id=$1", "purchaseId"))

	r.GET("/farms/:farmId/plantings", a.listByFarm("SELECT p.*, c.name crop_name, f.name field_name FROM plantings p JOIN crops c ON c.id=p.crop_id JOIN fields f ON f.id=p.field_id WHERE p.farm_id=$1 ORDER BY p.id DESC"))
	r.POST("/farms/:farmId/plantings", a.createPlanting)
	r.GET("/plantings/:plantingId", a.one("SELECT p.*, c.name crop_name, f.name field_name FROM plantings p JOIN crops c ON c.id=p.crop_id JOIN fields f ON f.id=p.field_id WHERE p.id=$1", "plantingId"))
	r.PATCH("/plantings/:plantingId", a.patchPlanting)
	r.POST("/plantings/:plantingId/complete", a.status("plantings", "plantingId", "harvested"))

	r.GET("/farms/:farmId/tasks", a.listByFarm("SELECT * FROM tasks WHERE farm_id=$1 ORDER BY due_date,id"))
	r.POST("/farms/:farmId/tasks", a.createTask)
	r.GET("/tasks/:taskId", a.one("SELECT * FROM tasks WHERE id=$1", "taskId"))
	r.PATCH("/tasks/:taskId", a.patchTask)
	r.POST("/tasks/:taskId/complete", a.status("tasks", "taskId", "done"))

	r.GET("/farms/:farmId/harvests", a.listByFarm("SELECT h.*, c.name crop_name FROM harvests h JOIN crops c ON c.id=h.crop_id WHERE h.farm_id=$1 ORDER BY h.id DESC"))
	r.POST("/farms/:farmId/harvests", a.createHarvest)
	r.GET("/harvests/:harvestId", a.one("SELECT h.*, c.name crop_name FROM harvests h JOIN crops c ON c.id=h.crop_id WHERE h.id=$1", "harvestId"))

	r.GET("/farms/:farmId/customers", a.listByFarm("SELECT * FROM customers WHERE farm_id=$1 ORDER BY id"))
	r.POST("/farms/:farmId/customers", a.createCustomer)
	r.GET("/customers/:customerId", a.one("SELECT * FROM customers WHERE id=$1", "customerId"))
	r.PATCH("/customers/:customerId", a.patchCustomer)
	r.DELETE("/customers/:customerId", a.delete("customers", "customerId"))

	r.GET("/farms/:farmId/sales", a.listByFarm("SELECT s.*, c.name customer_name, i.name item_name FROM sales s LEFT JOIN customers c ON c.id=s.customer_id LEFT JOIN inventory_items i ON i.id=s.inventory_item_id WHERE s.farm_id=$1 ORDER BY s.id DESC"))
	r.POST("/farms/:farmId/sales", a.createSale)
	r.GET("/sales/:saleId", a.one("SELECT * FROM sales WHERE id=$1", "saleId"))
	r.PATCH("/sales/:saleId", a.patchSale)

	r.GET("/farms/:farmId/dashboard", a.dashboard)
	r.GET("/farms/:farmId/analytics/profit-by-crop", a.listByFarm("SELECT c.name crop_name, COALESCE(sum(s.revenue_amount),0) revenue, COALESCE(sum(s.profit_amount),0) profit FROM crops c LEFT JOIN sales s ON s.crop_id=c.id WHERE c.farm_id=$1 GROUP BY c.id,c.name ORDER BY profit DESC"))
	r.GET("/farms/:farmId/analytics/inventory-summary", a.listByFarm("SELECT type, COALESCE(sum(quantity_kg),0) quantity_kg, count(*) items FROM inventory_items WHERE farm_id=$1 GROUP BY type ORDER BY type"))
	r.GET("/farms/:farmId/analytics/upcoming-tasks", a.listByFarm("SELECT * FROM tasks WHERE farm_id=$1 AND status<>'done' ORDER BY due_date,id LIMIT 10"))

	r.GET("/farms/:farmId/api-keys", a.listByFarm("SELECT id,farm_id,name,prefix,status,last_used_at,created_at,revoked_at FROM api_keys WHERE farm_id=$1 ORDER BY id DESC"))
	r.POST("/farms/:farmId/api-keys", a.createAPIKey)
	r.PATCH("/api-keys/:apiKeyId/revoke", a.status("api_keys", "apiKeyId", "revoked"))

	r.GET("/public/products", a.list("SELECT i.id, i.name, i.quantity_kg as available_kg, c.default_price_per_kg as price_per_kg, f.name as farm_name FROM inventory_items i JOIN crops c ON c.id=i.crop_id JOIN farms f ON f.id=i.farm_id WHERE i.type='harvest' AND i.quantity_kg>0 ORDER BY i.id"))
	r.POST("/public/orders", a.limiter.Middleware(40, time.Hour), a.publicOrder)
	r.POST("/public/leads", a.limiter.Middleware(20, time.Hour), a.publicLead)
	r.POST("/public/applications", a.limiter.Middleware(10, time.Hour), a.publicApplication)
}

func (a *API) register(c *gin.Context) {
	var in struct{ Name, Email, Password, FarmName, Phone string }
	if !httpx.Bind(c, &in) {
		return
	}
	if trim(in.Name, 120) == "" || !strings.Contains(in.Email, "@") || len(in.Password) < 6 {
		httpx.Error(c, 422, "VALIDATION_ERROR", "Имя, email и пароль от 6 символов обязательны")
		return
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	tx, _ := a.store.DB.Begin()
	defer tx.Rollback()
	var farmID int
	if err := tx.QueryRow("INSERT INTO farms(name,region) VALUES($1,$2) RETURNING id", value(in.FarmName, "Новая ферма"), "Чеченская Республика").Scan(&farmID); err != nil {
		httpx.Error(c, 500, "DB_ERROR", "Ошибка создания фермы")
		return
	}
	if _, err := tx.Exec("INSERT INTO users(farm_id,name,email,phone,password_hash) VALUES($1,$2,$3,$4,$5)", farmID, trim(in.Name, 120), strings.ToLower(trim(in.Email, 160)), trim(in.Phone, 40), string(hash)); err != nil {
		httpx.Error(c, 409, "EMAIL_EXISTS", "Пользователь уже существует")
		return
	}
	tx.Commit()
	c.JSON(201, gin.H{"farmId": farmID, "name": trim(in.Name, 120), "role": "owner"})
}

func (a *API) login(c *gin.Context) {
	var in struct{ Email, Password string }
	if !httpx.Bind(c, &in) {
		return
	}
	if in.Email == "demo" && in.Password == "demo" {
		c.JSON(200, gin.H{"farmId": 1, "name": "Демо", "role": "owner"})
		return
	}
	var farmID int
	var name, role, hash string
	err := a.store.DB.QueryRow("SELECT farm_id,name,role,password_hash FROM users WHERE email=$1", strings.ToLower(trim(in.Email, 160))).Scan(&farmID, &name, &role, &hash)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(hash), []byte(in.Password)) != nil {
		httpx.Error(c, 401, "INVALID_CREDENTIALS", "Неверный логин или пароль")
		return
	}
	c.JSON(200, gin.H{"farmId": farmID, "name": name, "role": role})
}

func (a *API) createFarm(c *gin.Context) {
	var in struct{ Name, Region, Address string }
	if !httpx.Bind(c, &in) {
		return
	}
	if trim(in.Name, 160) == "" {
		httpx.Error(c, 422, "VALIDATION_ERROR", "Название фермы обязательно")
		return
	}
	row := a.store.DB.QueryRow("INSERT INTO farms(name,region,address) VALUES($1,$2,$3) RETURNING id", trim(in.Name, 160), trim(in.Region, 120), trim(in.Address, 220))
	createdID(c, row)
}
func (a *API) patchFarm(c *gin.Context) {
	id, ok := idParam(c, "farmId")
	if ok {
		a.exec(c, "UPDATE farms SET name=COALESCE(NULLIF($1,''),name), region=COALESCE(NULLIF($2,''),region), address=COALESCE(NULLIF($3,''),address), updated_at=now() WHERE id=$4", readText(c, "name"), readText(c, "region"), readText(c, "address"), id)
	}
}

func (a *API) createField(c *gin.Context) {
	var in struct {
		Name, Location, SoilType, Status string
		AreaHectares                     float64 `json:"areaHectares"`
	}
	if !httpx.Bind(c, &in) {
		return
	}
	if trim(in.Name, 120) == "" || in.AreaHectares < 0 {
		httpx.Error(c, 422, "VALIDATION_ERROR", "Название и корректная площадь обязательны")
		return
	}
	row := a.store.DB.QueryRow("INSERT INTO fields(farm_id,name,area_hectares,location,soil_type,status) VALUES($1,$2,$3,$4,$5,$6) RETURNING id", c.Param("farmId"), trim(in.Name, 120), in.AreaHectares, trim(in.Location, 180), trim(in.SoilType, 80), value(in.Status, "ready"))
	createdID(c, row)
}
func (a *API) patchField(c *gin.Context) {
	id, ok := idParam(c, "fieldId")
	if !ok {
		return
	}
	var in struct {
		Name, Location, SoilType, Status string
		AreaHectares                     *float64 `json:"areaHectares"`
	}
	if !httpx.Bind(c, &in) {
		return
	}
	area := sql.NullFloat64{}
	if in.AreaHectares != nil {
		if *in.AreaHectares < 0 {
			httpx.Error(c, 422, "VALIDATION_ERROR", "Площадь не может быть отрицательной")
			return
		}
		area = sql.NullFloat64{Float64: *in.AreaHectares, Valid: true}
	}
	a.exec(c, "UPDATE fields SET name=COALESCE(NULLIF($1,''),name), location=COALESCE(NULLIF($2,''),location), soil_type=COALESCE(NULLIF($3,''),soil_type), status=COALESCE(NULLIF($4,''),status), area_hectares=COALESCE($5,area_hectares), updated_at=now() WHERE id=$6", trim(in.Name, 120), trim(in.Location, 180), trim(in.SoilType, 80), trim(in.Status, 40), area, id)
}

func (a *API) createCrop(c *gin.Context) {
	var in struct {
		Name, Category       string
		PricePerKg, SeedRate float64
	}
	if !httpx.Bind(c, &in) {
		return
	}
	if trim(in.Name, 120) == "" || in.PricePerKg < 0 {
		httpx.Error(c, 422, "VALIDATION_ERROR", "Название и цена обязательны")
		return
	}
	row := a.store.DB.QueryRow("INSERT INTO crops(farm_id,name,category,default_seed_rate_kg_per_hectare,default_price_per_kg) VALUES($1,$2,$3,$4,$5) RETURNING id", c.Param("farmId"), trim(in.Name, 120), trim(in.Category, 80), in.SeedRate, in.PricePerKg)
	createdID(c, row)
}
func (a *API) patchCrop(c *gin.Context) {
	id, ok := idParam(c, "cropId")
	if ok {
		a.exec(c, "UPDATE crops SET name=COALESCE(NULLIF($1,''),name), category=COALESCE(NULLIF($2,''),category), updated_at=now() WHERE id=$3", readText(c, "name"), readText(c, "category"), id)
	}
}

func (a *API) createInventory(c *gin.Context) {
	var in struct {
		Type, Name, Unit                            string
		CropID                                      int `json:"cropId"`
		QuantityKg, MinQuantityKg, AverageCostPerKg float64
	}
	if !httpx.Bind(c, &in) {
		return
	}
	if trim(in.Name, 140) == "" || !nonNegative(in.QuantityKg) {
		httpx.Error(c, 422, "VALIDATION_ERROR", "Название и количество обязательны")
		return
	}
	row := a.store.DB.QueryRow("INSERT INTO inventory_items(farm_id,type,name,crop_id,quantity_kg,unit,min_quantity_kg,average_cost_per_kg) VALUES($1,$2,$3,NULLIF($4,0),$5,$6,$7,$8) RETURNING id", c.Param("farmId"), value(in.Type, "other"), trim(in.Name, 140), in.CropID, in.QuantityKg, value(in.Unit, "кг"), in.MinQuantityKg, in.AverageCostPerKg)
	createdID(c, row)
}
func (a *API) patchInventory(c *gin.Context) {
	id, ok := idParam(c, "itemId")
	if ok {
		a.exec(c, "UPDATE inventory_items SET name=COALESCE(NULLIF($1,''),name), type=COALESCE(NULLIF($2,''),type), updated_at=now() WHERE id=$3", readText(c, "name"), readText(c, "type"), id)
	}
}
func (a *API) adjustInventory(c *gin.Context) {
	id, ok := idParam(c, "itemId")
	if !ok {
		return
	}
	var in struct{ DeltaKg float64 }
	if !httpx.Bind(c, &in) {
		return
	}
	if math.IsNaN(in.DeltaKg) || math.IsInf(in.DeltaKg, 0) {
		httpx.Error(c, 422, "VALIDATION_ERROR", "Некорректная корректировка")
		return
	}
	a.exec(c, "UPDATE inventory_items SET quantity_kg=GREATEST(quantity_kg+$1,0), updated_at=now() WHERE id=$2", in.DeltaKg, id)
}
func (a *API) createPurchase(c *gin.Context) {
	var in struct {
		InventoryItemID        int
		SupplierName           string
		QuantityKg, PricePerKg float64
	}
	if !httpx.Bind(c, &in) {
		return
	}
	if in.InventoryItemID <= 0 || in.QuantityKg <= 0 || in.PricePerKg < 0 {
		httpx.Error(c, 422, "VALIDATION_ERROR", "Товар, количество и цена обязательны")
		return
	}
	total := in.QuantityKg * in.PricePerKg
	tx, _ := a.store.DB.Begin()
	defer tx.Rollback()
	var id int
	if err := tx.QueryRow("INSERT INTO purchases(farm_id,inventory_item_id,supplier_name,quantity_kg,price_per_kg,total_amount) VALUES($1,$2,$3,$4,$5,$6) RETURNING id", c.Param("farmId"), in.InventoryItemID, trim(in.SupplierName, 160), in.QuantityKg, in.PricePerKg, total).Scan(&id); err != nil {
		httpx.Error(c, 500, "DB_ERROR", "Ошибка закупки")
		return
	}
	tx.Exec("UPDATE inventory_items SET quantity_kg=quantity_kg+$1, average_cost_per_kg=$2 WHERE id=$3", in.QuantityKg, in.PricePerKg, in.InventoryItemID)
	tx.Commit()
	c.JSON(201, gin.H{"id": id, "totalAmount": total})
}

func (a *API) createPlanting(c *gin.Context) {
	var in struct {
		FieldID, CropID                 int
		SeedQuantityKg, ExpectedYieldKg float64
	}
	if !httpx.Bind(c, &in) {
		return
	}
	if in.FieldID <= 0 || in.CropID <= 0 || in.SeedQuantityKg <= 0 {
		httpx.Error(c, 422, "VALIDATION_ERROR", "Поле, культура и семена обязательны")
		return
	}
	tx, _ := a.store.DB.Begin()
	defer tx.Rollback()
	var itemID int
	var qty, min float64
	err := tx.QueryRow("SELECT id,quantity_kg,min_quantity_kg FROM inventory_items WHERE farm_id=$1 AND crop_id=$2 AND type='seed' LIMIT 1 FOR UPDATE", c.Param("farmId"), in.CropID).Scan(&itemID, &qty, &min)
	if err != nil || qty < in.SeedQuantityKg {
		httpx.Error(c, 409, "INSUFFICIENT_SEED", "Недостаточно семян на складе")
		return
	}
	tx.Exec("UPDATE inventory_items SET quantity_kg=quantity_kg-$1 WHERE id=$2", in.SeedQuantityKg, itemID)
	var pid int
	tx.QueryRow("INSERT INTO plantings(farm_id,field_id,crop_id,seed_quantity_kg,expected_yield_kg,cost_amount) VALUES($1,$2,$3,$4,$5,$6) RETURNING id", c.Param("farmId"), in.FieldID, in.CropID, in.SeedQuantityKg, in.ExpectedYieldKg, in.SeedQuantityKg*18).Scan(&pid)
	for i, t := range []string{"Посадка картофеля", "Первый полив", "Обработка поля", "Проверка всходов"} {
		tx.Exec("INSERT INTO tasks(farm_id,field_id,planting_id,title,due_date) VALUES($1,$2,$3,$4,current_date+$5)", c.Param("farmId"), in.FieldID, pid, t, i+1)
	}
	tx.Commit()
	c.JSON(201, gin.H{"id": pid, "remainingSeedKg": qty - in.SeedQuantityKg, "warning": qty-in.SeedQuantityKg < min})
}
func (a *API) patchPlanting(c *gin.Context) {
	id, ok := idParam(c, "plantingId")
	if ok {
		a.exec(c, "UPDATE plantings SET status=COALESCE(NULLIF($1,''),status), updated_at=now() WHERE id=$2", readText(c, "status"), id)
	}
}
func (a *API) createTask(c *gin.Context) {
	var in struct {
		Title, Description, Type, Status, DueDate string
		FieldID, PlantingID                       int
	}
	if !httpx.Bind(c, &in) {
		return
	}
	if trim(in.Title, 180) == "" {
		httpx.Error(c, 422, "VALIDATION_ERROR", "Название задачи обязательно")
		return
	}
	row := a.store.DB.QueryRow("INSERT INTO tasks(farm_id,field_id,planting_id,title,description,type,status,due_date) VALUES($1,NULLIF($2,0),NULLIF($3,0),$4,$5,$6,$7,COALESCE(NULLIF($8,'')::date,current_date)) RETURNING id", c.Param("farmId"), in.FieldID, in.PlantingID, trim(in.Title, 180), trim(in.Description, 1000), value(in.Type, "planting"), value(in.Status, "todo"), trim(in.DueDate, 20))
	createdID(c, row)
}
func (a *API) patchTask(c *gin.Context) {
	id, ok := idParam(c, "taskId")
	if ok {
		a.exec(c, "UPDATE tasks SET title=COALESCE(NULLIF($1,''),title), status=COALESCE(NULLIF($2,''),status), updated_at=now() WHERE id=$3", readText(c, "title"), readText(c, "status"), id)
	}
}

func (a *API) createHarvest(c *gin.Context) {
	var in struct {
		PlantingID, CropID int
		QuantityKg         float64
		QualityGrade       string
	}
	if !httpx.Bind(c, &in) {
		return
	}
	if in.CropID <= 0 || in.QuantityKg <= 0 {
		httpx.Error(c, 422, "VALIDATION_ERROR", "Культура и количество обязательны")
		return
	}
	tx, _ := a.store.DB.Begin()
	defer tx.Rollback()
	var fieldID sql.NullInt64
	tx.QueryRow("SELECT field_id FROM plantings WHERE id=$1", in.PlantingID).Scan(&fieldID)
	var itemID int
	err := tx.QueryRow("SELECT id FROM inventory_items WHERE farm_id=$1 AND crop_id=$2 AND type='harvest' LIMIT 1 FOR UPDATE", c.Param("farmId"), in.CropID).Scan(&itemID)
	if err == sql.ErrNoRows {
		tx.QueryRow("INSERT INTO inventory_items(farm_id,type,name,crop_id,quantity_kg,min_quantity_kg,average_cost_per_kg) SELECT $1,'harvest',name,$2,$3,0,15 FROM crops WHERE id=$2 RETURNING id", c.Param("farmId"), in.CropID, in.QuantityKg).Scan(&itemID)
	} else if err == nil {
		tx.Exec("UPDATE inventory_items SET quantity_kg=quantity_kg+$1 WHERE id=$2", in.QuantityKg, itemID)
	} else {
		httpx.Error(c, 500, "DB_ERROR", "Ошибка склада")
		return
	}
	var id int
	tx.QueryRow("INSERT INTO harvests(farm_id,planting_id,field_id,crop_id,quantity_kg,quality_grade,added_to_inventory_item_id) VALUES($1,NULLIF($2,0),$3,$4,$5,$6,$7) RETURNING id", c.Param("farmId"), in.PlantingID, fieldID, in.CropID, in.QuantityKg, trim(in.QualityGrade, 80), itemID).Scan(&id)
	tx.Commit()
	c.JSON(201, gin.H{"id": id, "inventoryItemId": itemID})
}

func (a *API) createCustomer(c *gin.Context) {
	var in struct{ Name, Phone, Type, Email, Address, Notes string }
	if !httpx.Bind(c, &in) {
		return
	}
	if trim(in.Name, 160) == "" {
		httpx.Error(c, 422, "VALIDATION_ERROR", "Имя клиента обязательно")
		return
	}
	row := a.store.DB.QueryRow("INSERT INTO customers(farm_id,name,phone,type,email,address,notes) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING id", c.Param("farmId"), trim(in.Name, 160), trim(in.Phone, 40), value(in.Type, "restaurant"), trim(in.Email, 160), trim(in.Address, 220), trim(in.Notes, 500))
	createdID(c, row)
}
func (a *API) patchCustomer(c *gin.Context) {
	id, ok := idParam(c, "customerId")
	if ok {
		a.exec(c, "UPDATE customers SET name=COALESCE(NULLIF($1,''),name), phone=COALESCE(NULLIF($2,''),phone), updated_at=now() WHERE id=$3", readText(c, "name"), readText(c, "phone"), id)
	}
}
func (a *API) createSale(c *gin.Context) {
	var in struct {
		CustomerID, InventoryItemID int
		QuantityKg, PricePerKg      float64
	}
	if !httpx.Bind(c, &in) {
		return
	}
	if in.CustomerID <= 0 || in.InventoryItemID <= 0 || in.QuantityKg <= 0 || in.PricePerKg <= 0 {
		httpx.Error(c, 422, "VALIDATION_ERROR", "Клиент, товар, количество и цена обязательны")
		return
	}
	tx, _ := a.store.DB.Begin()
	defer tx.Rollback()
	var qty, cost float64
	var cropID sql.NullInt64
	err := tx.QueryRow("SELECT quantity_kg,average_cost_per_kg,crop_id FROM inventory_items WHERE id=$1 FOR UPDATE", in.InventoryItemID).Scan(&qty, &cost, &cropID)
	if err != nil || qty < in.QuantityKg {
		httpx.Error(c, 409, "INSUFFICIENT_PRODUCT", "Недостаточно продукции на складе")
		return
	}
	rev := in.QuantityKg * in.PricePerKg
	totalCost := in.QuantityKg * cost
	profit := rev - totalCost
	tx.Exec("UPDATE inventory_items SET quantity_kg=quantity_kg-$1 WHERE id=$2", in.QuantityKg, in.InventoryItemID)
	var id int
	tx.QueryRow("INSERT INTO sales(farm_id,customer_id,inventory_item_id,crop_id,quantity_kg,price_per_kg,revenue_amount,cost_amount,profit_amount) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id", c.Param("farmId"), in.CustomerID, in.InventoryItemID, cropID, in.QuantityKg, in.PricePerKg, rev, totalCost, profit).Scan(&id)
	tx.Commit()
	c.JSON(201, gin.H{"id": id, "revenue": rev, "profit": profit})
}
func (a *API) patchSale(c *gin.Context) {
	id, ok := idParam(c, "saleId")
	if ok {
		a.exec(c, "UPDATE sales SET status=COALESCE(NULLIF($1,''),status) WHERE id=$2", readText(c, "status"), id)
	}
}

func (a *API) dashboard(c *gin.Context) {
	farmID := c.Param("farmId")
	var revenue, profit, harvest, seed float64
	a.store.DB.QueryRow("SELECT COALESCE(sum(revenue_amount),0),COALESCE(sum(profit_amount),0) FROM sales WHERE farm_id=$1", farmID).Scan(&revenue, &profit)
	a.store.DB.QueryRow("SELECT COALESCE(sum(quantity_kg),0) FROM inventory_items WHERE farm_id=$1 AND type='harvest'", farmID).Scan(&harvest)
	a.store.DB.QueryRow("SELECT COALESCE(sum(quantity_kg),0) FROM inventory_items WHERE farm_id=$1 AND type='seed'", farmID).Scan(&seed)
	c.JSON(200, gin.H{"revenue": revenue, "profit": profit, "harvestKg": harvest, "seedKg": seed})
}
func (a *API) createAPIKey(c *gin.Context) {
	var in struct{ Name string }
	if !httpx.Bind(c, &in) {
		return
	}
	raw := make([]byte, 32)
	rand.Read(raw)
	token := "agro_" + base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(token))
	hash := base64.StdEncoding.EncodeToString(sum[:])
	prefix := token[:14]
	row := a.store.DB.QueryRow("INSERT INTO api_keys(farm_id,name,key_hash,prefix) VALUES($1,$2,$3,$4) RETURNING id", c.Param("farmId"), value(in.Name, "Интеграция"), hash, prefix)
	var id int
	if err := row.Scan(&id); err != nil {
		httpx.Error(c, 500, "DB_ERROR", "Ошибка создания ключа")
		return
	}
	c.JSON(201, gin.H{"id": id, "apiKey": token, "prefix": prefix})
}
func (a *API) publicOrder(c *gin.Context) {
	var in struct {
		CustomerName, Phone, Email string
		Items                      []struct {
			ProductID  int     `json:"productId"`
			QuantityKg float64 `json:"quantityKg"`
		}
	}
	if !httpx.Bind(c, &in) {
		return
	}
	if trim(in.CustomerName, 160) == "" || len(in.Items) == 0 {
		httpx.Error(c, 422, "VALIDATION_ERROR", "Имя клиента и товары обязательны")
		return
	}
	b, _ := json.Marshal(in.Items)
	totalQty, amount := 0.0, 0.0
	for _, it := range in.Items {
		if it.ProductID <= 0 || it.QuantityKg <= 0 {
			httpx.Error(c, 422, "VALIDATION_ERROR", "Некорректная позиция заказа")
			return
		}
		totalQty += it.QuantityKg
		var price float64
		a.store.DB.QueryRow("SELECT c.default_price_per_kg FROM inventory_items i JOIN crops c ON c.id=i.crop_id WHERE i.id=$1", it.ProductID).Scan(&price)
		amount += it.QuantityKg * price
	}
	row := a.store.DB.QueryRow("INSERT INTO external_orders(farm_id,customer_name,phone,email,items_json,total_quantity_kg,estimated_amount) VALUES((SELECT id FROM farms ORDER BY id LIMIT 1),$1,$2,$3,$4,$5,$6) RETURNING id", trim(in.CustomerName, 160), trim(in.Phone, 40), trim(in.Email, 160), string(b), totalQty, amount)
	var id int
	if err := row.Scan(&id); err != nil {
		httpx.Error(c, 500, "DB_ERROR", "Ошибка заказа")
		return
	}
	c.JSON(201, gin.H{"id": id, "status": "new", "estimatedAmount": amount})
}
func (a *API) publicLead(c *gin.Context) {
	var in struct{ Name, Phone, Email, Message string }
	if !httpx.Bind(c, &in) {
		return
	}
	if trim(in.Name, 160) == "" || trim(in.Phone, 40) == "" {
		httpx.Error(c, 422, "VALIDATION_ERROR", "Имя и телефон обязательны")
		return
	}
	payload, _ := json.Marshal([]map[string]string{{"message": trim(in.Message, 1000)}})
	row := a.store.DB.QueryRow("INSERT INTO external_orders(farm_id,customer_name,phone,email,source,items_json,status) VALUES((SELECT id FROM farms ORDER BY id LIMIT 1),$1,$2,$3,'lead',$4,'new') RETURNING id", trim(in.Name, 160), trim(in.Phone, 40), trim(in.Email, 160), string(payload))
	createdID(c, row)
}
func (a *API) publicApplication(c *gin.Context) {
	var in struct{ OwnerName, FarmName, Email, Phone, LandArea, BusinessScale, Region, Comment string }
	if !httpx.Bind(c, &in) {
		return
	}
	if trim(in.OwnerName, 160) == "" || trim(in.Phone, 40) == "" {
		httpx.Error(c, 422, "VALIDATION_ERROR", "ФИО и телефон обязательны")
		return
	}
	row := a.store.DB.QueryRow("INSERT INTO access_applications(owner_name,farm_name,email,phone,land_area,business_scale,region,comment) VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id", trim(in.OwnerName, 160), trim(in.FarmName, 160), trim(in.Email, 160), trim(in.Phone, 40), trim(in.LandArea, 80), trim(in.BusinessScale, 80), trim(in.Region, 120), trim(in.Comment, 1000))
	var id int
	if err := row.Scan(&id); err != nil {
		httpx.Error(c, 500, "DB_ERROR", "Ошибка заявки")
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": id, "status": "new", "message": "Заявка принята. Мы свяжемся с вами и выдадим доступ."})
}

func (a *API) list(query string) gin.HandlerFunc {
	return func(c *gin.Context) {
		items, err := a.store.Rows(query)
		if err != nil {
			httpx.Error(c, 500, "DB_ERROR", "Ошибка базы данных")
			return
		}
		c.JSON(200, items)
	}
}
func (a *API) listByFarm(query string) gin.HandlerFunc {
	return func(c *gin.Context) {
		items, err := a.store.Rows(query, c.Param("farmId"))
		if err != nil {
			httpx.Error(c, 500, "DB_ERROR", "Ошибка базы данных")
			return
		}
		c.JSON(200, items)
	}
}
func (a *API) one(query, param string) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := idParam(c, param)
		if !ok {
			return
		}
		item, found, err := a.store.One(query, id)
		if err != nil {
			httpx.Error(c, 500, "DB_ERROR", "Ошибка базы данных")
			return
		}
		if !found {
			httpx.Error(c, 404, "NOT_FOUND", "Запись не найдена")
			return
		}
		c.JSON(200, item)
	}
}
func (a *API) delete(table, param string) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := idParam(c, param)
		if ok {
			a.exec(c, "DELETE FROM "+table+" WHERE id=$1", id)
		}
	}
}
func (a *API) status(table, param, status string) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, ok := idParam(c, param)
		if ok {
			extra := ""
			if table == "api_keys" && status == "revoked" {
				extra = ", revoked_at=now()"
			}
			a.exec(c, "UPDATE "+table+" SET status=$1"+extra+" WHERE id=$2", status, id)
		}
	}
}
func (a *API) exec(c *gin.Context, query string, args ...any) {
	res, err := a.store.DB.Exec(query, args...)
	if err != nil {
		httpx.Error(c, 500, "DB_ERROR", "Ошибка базы данных")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		httpx.Error(c, 404, "NOT_FOUND", "Запись не найдена")
		return
	}
	c.JSON(200, gin.H{"ok": true})
}

func createdID(c *gin.Context, row *sql.Row) {
	var id int
	if err := row.Scan(&id); err != nil {
		httpx.Error(c, 500, "DB_ERROR", "Ошибка базы данных")
		return
	}
	c.JSON(201, gin.H{"id": id})
}
func idParam(c *gin.Context, name string) (int, bool) {
	id, err := strconv.Atoi(c.Param(name))
	if err != nil || id <= 0 {
		httpx.Error(c, 400, "INVALID_ID", "Некорректный идентификатор")
		return 0, false
	}
	return id, true
}
func trim(s string, max int) string {
	s = strings.TrimSpace(s)
	if max > 0 && len([]rune(s)) > max {
		r := []rune(s)
		return string(r[:max])
	}
	return s
}
func value(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return trim(v, 200)
}
func nonNegative(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 }
func readText(c *gin.Context, key string) string {
	var m map[string]any
	_ = c.ShouldBindJSON(&m)
	if v, ok := m[key]; ok {
		return trim(v.(string), 500)
	}
	return ""
}
