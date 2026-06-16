package handlers

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"agrocrm/backend/internal/httpx"
	"agrocrm/backend/internal/mailer"
	"agrocrm/backend/internal/store"

	"github.com/gin-gonic/gin"
	"github.com/xuri/excelize/v2"
	"golang.org/x/crypto/bcrypt"
)

type API struct {
	store   *store.Store
	limiter *httpx.Limiter
	mailer  *mailer.Client
}

func New(store *store.Store, limiter *httpx.Limiter, mailer *mailer.Client) *API {
	return &API{store: store, limiter: limiter, mailer: mailer}
}

func (a *API) RegisterRoutes(r *gin.Engine) {
	r.GET("/health", func(c *gin.Context) { c.JSON(200, gin.H{"ok": true}) })

	// Swagger UI
	r.GET("/swagger/doc.yaml", swaggerDocHandler)
	r.GET("/swagger/index.html", swaggerUIHandler)
	r.GET("/swagger", swaggerUIHandler)
	r.POST("/auth/register", a.limiter.Middleware(8, 15*time.Minute), a.register)
	r.POST("/auth/login", a.limiter.Middleware(12, 15*time.Minute), a.login)
	r.GET("/auth/me", a.me)
	r.PATCH("/auth/me", a.patchMe)

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
	r.DELETE("/inventory/:itemId", a.delete("inventory_items", "itemId"))
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
	r.DELETE("/plantings/:plantingId", a.deletePlanting)

	r.GET("/farms/:farmId/tasks", a.listByFarm("SELECT * FROM tasks WHERE farm_id=$1 ORDER BY due_date,id"))
	r.POST("/farms/:farmId/tasks", a.createTask)
	r.GET("/tasks/:taskId", a.one("SELECT * FROM tasks WHERE id=$1", "taskId"))
	r.PATCH("/tasks/:taskId", a.patchTask)
	r.POST("/tasks/:taskId/complete", a.status("tasks", "taskId", "done"))
	r.DELETE("/tasks/:taskId", a.delete("tasks", "taskId"))

	r.GET("/farms/:farmId/harvests", a.listByFarm("SELECT h.*, c.name crop_name FROM harvests h JOIN crops c ON c.id=h.crop_id WHERE h.farm_id=$1 ORDER BY h.id DESC"))
	r.POST("/farms/:farmId/harvests", a.createHarvest)
	r.GET("/harvests/:harvestId", a.one("SELECT h.*, c.name crop_name FROM harvests h JOIN crops c ON c.id=h.crop_id WHERE h.id=$1", "harvestId"))
	r.PATCH("/harvests/:harvestId", a.patchHarvest)
	r.DELETE("/harvests/:harvestId", a.deleteHarvest)

	r.GET("/farms/:farmId/customers", a.listByFarm("SELECT * FROM customers WHERE farm_id=$1 ORDER BY id"))
	r.POST("/farms/:farmId/customers", a.createCustomer)
	r.GET("/customers/:customerId", a.one("SELECT * FROM customers WHERE id=$1", "customerId"))
	r.PATCH("/customers/:customerId", a.patchCustomer)
	r.DELETE("/customers/:customerId", a.delete("customers", "customerId"))

	r.GET("/farms/:farmId/sales", a.listByFarm("SELECT s.*, c.name customer_name, i.name item_name FROM sales s LEFT JOIN customers c ON c.id=s.customer_id LEFT JOIN inventory_items i ON i.id=s.inventory_item_id WHERE s.farm_id=$1 ORDER BY s.id DESC"))
	r.POST("/farms/:farmId/sales", a.createSale)
	r.GET("/sales/:saleId", a.one("SELECT * FROM sales WHERE id=$1", "saleId"))
	r.PATCH("/sales/:saleId", a.patchSale)
	r.DELETE("/sales/:saleId", a.deleteSale)

	r.GET("/farms/:farmId/dashboard", a.dashboard)
	r.GET("/farms/:farmId/export/:kind", a.exportXLSX)
	r.GET("/farms/:farmId/analytics/profit-by-crop", a.listByFarm("SELECT c.name crop_name, COALESCE(sum(s.revenue_amount),0) revenue, COALESCE(sum(s.profit_amount),0) profit FROM crops c LEFT JOIN sales s ON s.crop_id=c.id WHERE c.farm_id=$1 GROUP BY c.id,c.name ORDER BY profit DESC"))
	r.GET("/farms/:farmId/analytics/inventory-summary", a.listByFarm("SELECT type, COALESCE(sum(quantity_kg),0) quantity_kg, count(*) items FROM inventory_items WHERE farm_id=$1 GROUP BY type ORDER BY type"))
	r.GET("/farms/:farmId/analytics/upcoming-tasks", a.listByFarm("SELECT * FROM tasks WHERE farm_id=$1 AND status<>'done' ORDER BY due_date,id LIMIT 10"))

	r.GET("/farms/:farmId/api-keys", a.listByFarm("SELECT id,farm_id,name,prefix,status,last_used_at,created_at,revoked_at FROM api_keys WHERE farm_id=$1 ORDER BY id DESC"))
	r.POST("/farms/:farmId/api-keys", a.createAPIKey)
	r.PATCH("/api-keys/:apiKeyId/revoke", a.status("api_keys", "apiKeyId", "revoked"))
	r.POST("/farms/:farmId/import/:kind", a.importXLSX)
	r.GET("/admin/summary", a.adminSummary)
	r.GET("/admin/applications", a.adminApplications)
	r.PATCH("/admin/applications/:applicationId", a.adminPatchApplication)
	r.GET("/admin/orders", a.adminOrders)
	r.PATCH("/admin/orders/:orderId", a.adminPatchOrder)

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
	if strings.ToLower(trim(in.Email, 160)) == "admin" {
		in.Email = "admin@agrocrm.local"
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

func (a *API) me(c *gin.Context) {
	var farmID int
	var name, role, email, phone string
	err := a.store.DB.QueryRow("SELECT farm_id,name,role,email,phone FROM users ORDER BY id LIMIT 1").Scan(&farmID, &name, &role, &email, &phone)
	if err == nil {
		c.JSON(200, gin.H{"farmId": farmID, "name": name, "role": role, "email": email, "phone": phone})
		return
	}
	c.JSON(200, gin.H{"farmId": 1, "name": "Ахмат Исаев", "role": "owner", "email": "", "phone": ""})
}

func (a *API) patchMe(c *gin.Context) {
	var in struct {
		FarmID             int `json:"farmId"`
		Name, Email, Phone string
	}
	if !httpx.Bind(c, &in) {
		return
	}
	if in.FarmID <= 0 {
		in.FarmID = 1
	}
	a.exec(c, "UPDATE users SET name=COALESCE(NULLIF($1,''),name), email=COALESCE(NULLIF($2,''),email), phone=COALESCE(NULLIF($3,''),phone), updated_at=now() WHERE id=(SELECT id FROM users WHERE farm_id=$4 ORDER BY id LIMIT 1)", trim(in.Name, 120), trim(in.Email, 160), trim(in.Phone, 40), in.FarmID)
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
	if !ok {
		return
	}
	var in struct {
		Type, Name, Unit                            string
		CropID                                      *int `json:"cropId"`
		QuantityKg, MinQuantityKg, AverageCostPerKg *float64
	}
	if !httpx.Bind(c, &in) {
		return
	}
	var cropID sql.NullInt64
	var quantity, minQty, avgCost sql.NullFloat64
	if in.CropID != nil {
		cropID = sql.NullInt64{Int64: int64(*in.CropID), Valid: *in.CropID > 0}
	}
	if in.QuantityKg != nil {
		quantity = sql.NullFloat64{Float64: *in.QuantityKg, Valid: true}
	}
	if in.MinQuantityKg != nil {
		minQty = sql.NullFloat64{Float64: *in.MinQuantityKg, Valid: true}
	}
	if in.AverageCostPerKg != nil {
		avgCost = sql.NullFloat64{Float64: *in.AverageCostPerKg, Valid: true}
	}
	a.exec(c, "UPDATE inventory_items SET name=COALESCE(NULLIF($1,''),name), type=COALESCE(NULLIF($2,''),type), unit=COALESCE(NULLIF($3,''),unit), crop_id=COALESCE($4,crop_id), quantity_kg=COALESCE($5,quantity_kg), min_quantity_kg=COALESCE($6,min_quantity_kg), average_cost_per_kg=COALESCE($7,average_cost_per_kg), updated_at=now() WHERE id=$8", trim(in.Name, 140), trim(in.Type, 60), trim(in.Unit, 20), cropID, quantity, minQty, avgCost, id)
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
	if _, err := tx.Exec("UPDATE inventory_items SET quantity_kg=quantity_kg-$1 WHERE id=$2", in.SeedQuantityKg, itemID); err != nil {
		httpx.Error(c, 500, "DB_ERROR", "Ошибка обновления склада")
		return
	}
	var pid int
	if err := tx.QueryRow("INSERT INTO plantings(farm_id,field_id,crop_id,seed_quantity_kg,expected_yield_kg,cost_amount) VALUES($1,$2,$3,$4,$5,$6) RETURNING id", c.Param("farmId"), in.FieldID, in.CropID, in.SeedQuantityKg, in.ExpectedYieldKg, in.SeedQuantityKg*18).Scan(&pid); err != nil {
		httpx.Error(c, 500, "DB_ERROR", "Ошибка создания посева")
		return
	}
	for i, t := range []string{"Посадка картофеля", "Первый полив", "Обработка поля", "Проверка всходов"} {
		if _, err := tx.Exec("INSERT INTO tasks(farm_id,field_id,planting_id,title,due_date) VALUES($1,$2,$3,$4,current_date + ($5 * INTERVAL '1 day'))", c.Param("farmId"), in.FieldID, pid, t, i+1); err != nil {
			httpx.Error(c, 500, "DB_ERROR", "Ошибка создания задач по посеву")
			return
		}
	}
	if err := tx.Commit(); err != nil {
		httpx.Error(c, 500, "DB_ERROR", "Ошибка сохранения посева")
		return
	}
	c.JSON(201, gin.H{"id": pid, "remainingSeedKg": qty - in.SeedQuantityKg, "warning": qty-in.SeedQuantityKg < min})
}
func (a *API) patchPlanting(c *gin.Context) {
	id, ok := idParam(c, "plantingId")
	if !ok {
		return
	}
	var in struct {
		FieldID, CropID                 *int     `json:"fieldID"`
		SeedQuantityKg, ExpectedYieldKg *float64 `json:"seedQuantityKg"`
		Status                          string   `json:"status"`
		PlantingDate                    string   `json:"plantingDate"`
		PlannedHarvestDate              string   `json:"plannedHarvestDate"`
	}
	if !httpx.Bind(c, &in) {
		return
	}
	if in.FieldID != nil && *in.FieldID <= 0 || in.CropID != nil && *in.CropID <= 0 || in.SeedQuantityKg != nil && *in.SeedQuantityKg < 0 || in.ExpectedYieldKg != nil && *in.ExpectedYieldKg < 0 {
		httpx.Error(c, 422, "VALIDATION_ERROR", "Некорректные данные посева")
		return
	}
	tx, _ := a.store.DB.Begin()
	defer tx.Rollback()
	var oldCropID int
	var oldSeedQty float64
	if err := tx.QueryRow("SELECT crop_id,seed_quantity_kg FROM plantings WHERE id=$1 FOR UPDATE", id).Scan(&oldCropID, &oldSeedQty); err != nil {
		if err == sql.ErrNoRows {
			httpx.Error(c, 404, "NOT_FOUND", "Посев не найден")
		} else {
			httpx.Error(c, 500, "DB_ERROR", "Ошибка базы данных")
		}
		return
	}
	newCropID := oldCropID
	if in.CropID != nil {
		newCropID = *in.CropID
	}
	newSeedQty := oldSeedQty
	if in.SeedQuantityKg != nil {
		newSeedQty = *in.SeedQuantityKg
	}
	if newCropID != oldCropID || newSeedQty != oldSeedQty {
		if _, err := tx.Exec("UPDATE inventory_items SET quantity_kg=quantity_kg+$1, updated_at=now() WHERE crop_id=$2 AND type='seed' AND farm_id=(SELECT farm_id FROM plantings WHERE id=$3)", oldSeedQty, oldCropID, id); err != nil {
			httpx.Error(c, 500, "DB_ERROR", "Ошибка возврата семян")
			return
		}
		var itemID int
		var qty float64
		if err := tx.QueryRow("SELECT id,quantity_kg FROM inventory_items WHERE crop_id=$1 AND type='seed' AND farm_id=(SELECT farm_id FROM plantings WHERE id=$2) LIMIT 1 FOR UPDATE", newCropID, id).Scan(&itemID, &qty); err != nil || qty < newSeedQty {
			httpx.Error(c, 409, "INSUFFICIENT_SEED", "Недостаточно семян на складе для обновления посева")
			return
		}
		tx.Exec("UPDATE inventory_items SET quantity_kg=quantity_kg-$1, updated_at=now() WHERE id=$2", newSeedQty, itemID)
	}
	fieldID, cropID, seedQty, expectedYield := sql.NullInt64{}, sql.NullInt64{}, sql.NullFloat64{}, sql.NullFloat64{}
	if in.FieldID != nil {
		fieldID = sql.NullInt64{Int64: int64(*in.FieldID), Valid: true}
	}
	if in.CropID != nil {
		cropID = sql.NullInt64{Int64: int64(*in.CropID), Valid: true}
	}
	if in.SeedQuantityKg != nil {
		seedQty = sql.NullFloat64{Float64: *in.SeedQuantityKg, Valid: true}
	}
	if in.ExpectedYieldKg != nil {
		expectedYield = sql.NullFloat64{Float64: *in.ExpectedYieldKg, Valid: true}
	}
	res, err := tx.Exec("UPDATE plantings SET field_id=COALESCE($1,field_id), crop_id=COALESCE($2,crop_id), seed_quantity_kg=COALESCE($3,seed_quantity_kg), expected_yield_kg=COALESCE($4,expected_yield_kg), status=COALESCE(NULLIF($5,''),status), planting_date=COALESCE(NULLIF($6,'')::date,planting_date), planned_harvest_date=COALESCE(NULLIF($7,'')::date,planned_harvest_date), cost_amount=COALESCE($3,seed_quantity_kg)*18, updated_at=now() WHERE id=$8", fieldID, cropID, seedQty, expectedYield, trim(in.Status, 40), trim(in.PlantingDate, 20), trim(in.PlannedHarvestDate, 20), id)
	if err != nil {
		httpx.Error(c, 500, "DB_ERROR", "Ошибка обновления посева")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		httpx.Error(c, 404, "NOT_FOUND", "Посев не найден")
		return
	}
	tx.Commit()
	c.JSON(200, gin.H{"ok": true})
}
func (a *API) deletePlanting(c *gin.Context) {
	id, ok := idParam(c, "plantingId")
	if !ok {
		return
	}
	tx, _ := a.store.DB.Begin()
	defer tx.Rollback()
	var farmID, cropID int
	var seedQty float64
	if err := tx.QueryRow("SELECT farm_id,crop_id,seed_quantity_kg FROM plantings WHERE id=$1 FOR UPDATE", id).Scan(&farmID, &cropID, &seedQty); err != nil {
		if err == sql.ErrNoRows {
			httpx.Error(c, 404, "NOT_FOUND", "Посев не найден")
		} else {
			httpx.Error(c, 500, "DB_ERROR", "Ошибка базы данных")
		}
		return
	}
	if _, err := tx.Exec("UPDATE inventory_items SET quantity_kg=quantity_kg+$1, updated_at=now() WHERE farm_id=$2 AND crop_id=$3 AND type='seed'", seedQty, farmID, cropID); err != nil {
		httpx.Error(c, 500, "DB_ERROR", "Ошибка возврата семян")
		return
	}
	if _, err := tx.Exec("UPDATE harvests SET planting_id=NULL WHERE planting_id=$1", id); err != nil {
		httpx.Error(c, 500, "DB_ERROR", "Ошибка удаления связанного урожая")
		return
	}
	if _, err := tx.Exec("DELETE FROM tasks WHERE planting_id=$1", id); err != nil {
		httpx.Error(c, 500, "DB_ERROR", "Ошибка удаления связанных задач")
		return
	}
	res, err := tx.Exec("DELETE FROM plantings WHERE id=$1", id)
	if err != nil {
		httpx.Error(c, 500, "DB_ERROR", "Ошибка удаления посева")
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		httpx.Error(c, 404, "NOT_FOUND", "Посев не найден")
		return
	}
	tx.Commit()
	c.JSON(200, gin.H{"ok": true})
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
	if !ok {
		return
	}
	var in struct {
		Title, Description, Type, Status, DueDate string
		FieldID, PlantingID                       *int `json:"fieldID"`
	}
	if !httpx.Bind(c, &in) {
		return
	}
	fieldID, plantingID := sql.NullInt64{}, sql.NullInt64{}
	if in.FieldID != nil {
		fieldID = sql.NullInt64{Int64: int64(*in.FieldID), Valid: *in.FieldID > 0}
	}
	if in.PlantingID != nil {
		plantingID = sql.NullInt64{Int64: int64(*in.PlantingID), Valid: *in.PlantingID > 0}
	}
	a.exec(c, "UPDATE tasks SET title=COALESCE(NULLIF($1,''),title), description=COALESCE(NULLIF($2,''),description), type=COALESCE(NULLIF($3,''),type), status=COALESCE(NULLIF($4,''),status), due_date=COALESCE(NULLIF($5,'')::date,due_date), field_id=COALESCE($6,field_id), planting_id=COALESCE($7,planting_id), updated_at=now() WHERE id=$8", trim(in.Title, 180), trim(in.Description, 1000), trim(in.Type, 80), trim(in.Status, 40), trim(in.DueDate, 20), fieldID, plantingID, id)
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
	if in.PlantingID > 0 {
		if err := tx.QueryRow("SELECT field_id FROM plantings WHERE id=$1", in.PlantingID).Scan(&fieldID); err != nil && err != sql.ErrNoRows {
			httpx.Error(c, 500, "DB_ERROR", "Ошибка загрузки посева для урожая")
			return
		}
	}
	var itemID int
	err := tx.QueryRow("SELECT id FROM inventory_items WHERE farm_id=$1 AND crop_id=$2 AND type='harvest' LIMIT 1 FOR UPDATE", c.Param("farmId"), in.CropID).Scan(&itemID)
	if err == sql.ErrNoRows {
		if err := tx.QueryRow("INSERT INTO inventory_items(farm_id,type,name,crop_id,quantity_kg,min_quantity_kg,average_cost_per_kg) SELECT $1,'harvest',name,$2,$3,0,15 FROM crops WHERE id=$2 RETURNING id", c.Param("farmId"), in.CropID, in.QuantityKg).Scan(&itemID); err != nil {
			httpx.Error(c, 500, "DB_ERROR", "Ошибка создания складской позиции урожая")
			return
		}
	} else if err == nil {
		if _, err := tx.Exec("UPDATE inventory_items SET quantity_kg=quantity_kg+$1 WHERE id=$2", in.QuantityKg, itemID); err != nil {
			httpx.Error(c, 500, "DB_ERROR", "Ошибка обновления склада урожая")
			return
		}
	} else {
		httpx.Error(c, 500, "DB_ERROR", "Ошибка склада")
		return
	}
	var id int
	if err := tx.QueryRow("INSERT INTO harvests(farm_id,planting_id,field_id,crop_id,quantity_kg,quality_grade,added_to_inventory_item_id) VALUES($1,NULLIF($2,0),$3,$4,$5,$6,$7) RETURNING id", c.Param("farmId"), in.PlantingID, fieldID, in.CropID, in.QuantityKg, trim(in.QualityGrade, 80), itemID).Scan(&id); err != nil {
		httpx.Error(c, 500, "DB_ERROR", "Ошибка создания записи урожая")
		return
	}
	if err := tx.Commit(); err != nil {
		httpx.Error(c, 500, "DB_ERROR", "Ошибка сохранения урожая")
		return
	}
	c.JSON(201, gin.H{"id": id, "inventoryItemId": itemID})
}
func (a *API) patchHarvest(c *gin.Context) {
	id, ok := idParam(c, "harvestId")
	if !ok {
		return
	}
	var in struct {
		QualityGrade string
	}
	if !httpx.Bind(c, &in) {
		return
	}
	a.exec(c, "UPDATE harvests SET quality_grade=COALESCE(NULLIF($1,''),quality_grade) WHERE id=$2", trim(in.QualityGrade, 80), id)
}
func (a *API) deleteHarvest(c *gin.Context) {
	id, ok := idParam(c, "harvestId")
	if !ok {
		return
	}
	tx, _ := a.store.DB.Begin()
	defer tx.Rollback()
	var itemID int
	var qty float64
	if err := tx.QueryRow("SELECT added_to_inventory_item_id,quantity_kg FROM harvests WHERE id=$1 FOR UPDATE", id).Scan(&itemID, &qty); err != nil {
		if err == sql.ErrNoRows {
			httpx.Error(c, 404, "NOT_FOUND", "Урожай не найден")
		} else {
			httpx.Error(c, 500, "DB_ERROR", "Ошибка базы данных")
		}
		return
	}
	if _, err := tx.Exec("UPDATE inventory_items SET quantity_kg=GREATEST(quantity_kg-$1,0), updated_at=now() WHERE id=$2", qty, itemID); err != nil {
		httpx.Error(c, 500, "DB_ERROR", "Ошибка отката склада")
		return
	}
	if _, err := tx.Exec("DELETE FROM harvests WHERE id=$1", id); err != nil {
		httpx.Error(c, 500, "DB_ERROR", "Ошибка удаления урожая")
		return
	}
	tx.Commit()
	c.JSON(200, gin.H{"ok": true})
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
	if !ok {
		return
	}
	var in struct{ Name, Phone, Type, Email, Address, Notes string }
	if !httpx.Bind(c, &in) {
		return
	}
	a.exec(c, "UPDATE customers SET name=COALESCE(NULLIF($1,''),name), phone=COALESCE(NULLIF($2,''),phone), type=COALESCE(NULLIF($3,''),type), email=COALESCE(NULLIF($4,''),email), address=COALESCE(NULLIF($5,''),address), notes=COALESCE(NULLIF($6,''),notes), updated_at=now() WHERE id=$7", trim(in.Name, 160), trim(in.Phone, 40), trim(in.Type, 40), trim(in.Email, 160), trim(in.Address, 220), trim(in.Notes, 500), id)
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
	if !ok {
		return
	}
	var in struct {
		Status string
	}
	if !httpx.Bind(c, &in) {
		return
	}
	a.exec(c, "UPDATE sales SET status=COALESCE(NULLIF($1,''),status) WHERE id=$2", trim(in.Status, 40), id)
}
func (a *API) deleteSale(c *gin.Context) {
	id, ok := idParam(c, "saleId")
	if !ok {
		return
	}
	tx, _ := a.store.DB.Begin()
	defer tx.Rollback()
	var inventoryItemID int
	var qty float64
	if err := tx.QueryRow("SELECT inventory_item_id,quantity_kg FROM sales WHERE id=$1 FOR UPDATE", id).Scan(&inventoryItemID, &qty); err != nil {
		if err == sql.ErrNoRows {
			httpx.Error(c, 404, "NOT_FOUND", "Продажа не найдена")
		} else {
			httpx.Error(c, 500, "DB_ERROR", "Ошибка базы данных")
		}
		return
	}
	if _, err := tx.Exec("UPDATE inventory_items SET quantity_kg=quantity_kg+$1, updated_at=now() WHERE id=$2", qty, inventoryItemID); err != nil {
		httpx.Error(c, 500, "DB_ERROR", "Ошибка возврата товара на склад")
		return
	}
	if _, err := tx.Exec("DELETE FROM sales WHERE id=$1", id); err != nil {
		httpx.Error(c, 500, "DB_ERROR", "Ошибка удаления продажи")
		return
	}
	tx.Commit()
	c.JSON(200, gin.H{"ok": true})
}

func (a *API) importXLSX(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		httpx.Error(c, 400, "FILE_REQUIRED", "Нужно приложить XLSX файл")
		return
	}
	src, err := file.Open()
	if err != nil {
		httpx.Error(c, 400, "FILE_ERROR", "Не удалось открыть файл")
		return
	}
	defer src.Close()
	wb, err := excelize.OpenReader(src)
	if err != nil {
		httpx.Error(c, 400, "INVALID_XLSX", "Не удалось прочитать XLSX файл")
		return
	}
	defer wb.Close()
	sheets := wb.GetSheetList()
	if len(sheets) == 0 {
		httpx.Error(c, 400, "EMPTY_XLSX", "В файле нет листов")
		return
	}
	rows, err := wb.GetRows(sheets[0])
	if err != nil || len(rows) < 2 {
		httpx.Error(c, 400, "EMPTY_XLSX", "В файле нет данных")
		return
	}
	headers := map[string]int{}
	for i, cell := range rows[0] {
		headers[normalizeHeader(cell)] = i
	}
	tx, _ := a.store.DB.Begin()
	defer tx.Rollback()
	created := 0
	for _, row := range rows[1:] {
		if isRowEmpty(row) {
			continue
		}
		switch c.Param("kind") {
		case "inventory":
			_, err = tx.Exec("INSERT INTO inventory_items(farm_id,type,name,crop_id,quantity_kg,unit,min_quantity_kg,average_cost_per_kg) VALUES($1,$2,$3,NULLIF($4,0),$5,$6,$7,$8)", c.Param("farmId"), rowValue(row, headers, "type", "тип"), rowValue(row, headers, "name", "название"), atoi(rowValue(row, headers, "cropid", "crop_id")), atof(rowValue(row, headers, "quantitykg", "quantity_kg", "количество")), value(rowValue(row, headers, "unit", "ед"), "кг"), atof(rowValue(row, headers, "minquantitykg", "min_quantity_kg", "минимум")), atof(rowValue(row, headers, "averagecostperkg", "average_cost_per_kg", "цена")))
		case "plantings":
			cropID := atoi(rowValue(row, headers, "cropid", "crop_id"))
			fieldID := atoi(rowValue(row, headers, "fieldid", "field_id"))
			seedQty := atof(rowValue(row, headers, "seedquantitykg", "seed_quantity_kg", "семена"))
			expected := atof(rowValue(row, headers, "expectedyieldkg", "expected_yield_kg", "урожай"))
			status := value(rowValue(row, headers, "status", "статус"), "active")
			var itemID int
			var qty float64
			if err = tx.QueryRow("SELECT id,quantity_kg FROM inventory_items WHERE farm_id=$1 AND crop_id=$2 AND type='seed' LIMIT 1 FOR UPDATE", c.Param("farmId"), cropID).Scan(&itemID, &qty); err != nil || qty < seedQty {
				httpx.Error(c, 409, "INSUFFICIENT_SEED", "Недостаточно семян для импорта посевов")
				return
			}
			if _, err = tx.Exec("UPDATE inventory_items SET quantity_kg=quantity_kg-$1, updated_at=now() WHERE id=$2", seedQty, itemID); err == nil {
				_, err = tx.Exec("INSERT INTO plantings(farm_id,field_id,crop_id,planting_date,planned_harvest_date,seed_quantity_kg,expected_yield_kg,status,cost_amount) VALUES($1,$2,$3,COALESCE(NULLIF($4,'')::date,current_date),NULLIF($5,'')::date,$6,$7,$8,$9)", c.Param("farmId"), fieldID, cropID, rowValue(row, headers, "plantingdate", "planting_date"), rowValue(row, headers, "plannedharvestdate", "planned_harvest_date"), seedQty, expected, status, seedQty*18)
			}
		case "customers":
			_, err = tx.Exec("INSERT INTO customers(farm_id,name,type,phone,email,address,notes) VALUES($1,$2,$3,$4,$5,$6,$7)", c.Param("farmId"), rowValue(row, headers, "name", "имя"), value(rowValue(row, headers, "type", "тип"), "restaurant"), rowValue(row, headers, "phone", "телефон"), rowValue(row, headers, "email", "почта"), rowValue(row, headers, "address", "адрес"), rowValue(row, headers, "notes", "заметки"))
		case "fields":
			_, err = tx.Exec("INSERT INTO fields(farm_id,name,area_hectares,location,soil_type,status) VALUES($1,$2,$3,$4,$5,$6)", c.Param("farmId"), rowValue(row, headers, "name", "название"), atof(rowValue(row, headers, "areahectares", "area_hectares", "площадь")), rowValue(row, headers, "location", "локация"), rowValue(row, headers, "soiltype", "soil_type", "почва"), value(rowValue(row, headers, "status", "статус"), "ready"))
		default:
			httpx.Error(c, 400, "UNSUPPORTED_IMPORT", "Импорт для этого раздела пока не поддерживается")
			return
		}
		if err != nil {
			httpx.Error(c, 500, "IMPORT_ERROR", "Ошибка импорта данных")
			return
		}
		created++
	}
	tx.Commit()
	c.JSON(200, gin.H{"ok": true, "created": created})
}

func (a *API) dashboard(c *gin.Context) {
	farmID := c.Param("farmId")
	var revenue, profit, harvest, seed float64
	a.store.DB.QueryRow("SELECT COALESCE(sum(revenue_amount),0),COALESCE(sum(profit_amount),0) FROM sales WHERE farm_id=$1", farmID).Scan(&revenue, &profit)
	a.store.DB.QueryRow("SELECT COALESCE(sum(quantity_kg),0) FROM inventory_items WHERE farm_id=$1 AND type='harvest'", farmID).Scan(&harvest)
	a.store.DB.QueryRow("SELECT COALESCE(sum(quantity_kg),0) FROM inventory_items WHERE farm_id=$1 AND type='seed'", farmID).Scan(&seed)
	c.JSON(200, gin.H{"revenue": revenue, "profit": profit, "harvestKg": harvest, "seedKg": seed})
}

func (a *API) exportXLSX(c *gin.Context) {
	kind := c.Param("kind")
	query, filename := exportQuery(kind)
	if query == "" {
		httpx.Error(c, 400, "UNSUPPORTED_EXPORT", "Экспорт для этого раздела не поддерживается")
		return
	}
	rows, err := a.store.Rows(query, c.Param("farmId"))
	if err != nil {
		httpx.Error(c, 500, "DB_ERROR", "Ошибка подготовки экспорта")
		return
	}
	wb := excelize.NewFile()
	sheet := wb.GetSheetName(0)
	if len(rows) > 0 {
		headers := sortedKeys(rows[0])
		for i, header := range headers {
			cell, _ := excelize.CoordinatesToCellName(i+1, 1)
			wb.SetCellValue(sheet, cell, header)
		}
		for rowIndex, row := range rows {
			for colIndex, header := range headers {
				cell, _ := excelize.CoordinatesToCellName(colIndex+1, rowIndex+2)
				wb.SetCellValue(sheet, cell, stringify(row[header]))
			}
		}
	}
	buf := &bytes.Buffer{}
	if err := wb.Write(buf); err != nil {
		httpx.Error(c, 500, "EXPORT_ERROR", "Ошибка формирования XLSX")
		return
	}
	c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Header("Content-Disposition", "attachment; filename="+filename)
	c.Data(200, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", buf.Bytes())
}

func exportQuery(kind string) (string, string) {
	switch kind {
	case "fields":
		return "SELECT id,name,area_hectares,location,soil_type,status,created_at,updated_at FROM fields WHERE farm_id=$1 ORDER BY id", "fields.xlsx"
	case "inventory":
		return "SELECT id,type,name,crop_id,quantity_kg,unit,min_quantity_kg,average_cost_per_kg,created_at,updated_at FROM inventory_items WHERE farm_id=$1 ORDER BY id", "inventory.xlsx"
	case "plantings":
		return "SELECT p.id,c.name crop_name,f.name field_name,p.planting_date,p.planned_harvest_date,p.seed_quantity_kg,p.expected_yield_kg,p.status,p.created_at,p.updated_at FROM plantings p JOIN crops c ON c.id=p.crop_id JOIN fields f ON f.id=p.field_id WHERE p.farm_id=$1 ORDER BY p.id DESC", "plantings.xlsx"
	case "tasks":
		return "SELECT id,title,description,type,status,due_date,field_id,planting_id,created_at,updated_at FROM tasks WHERE farm_id=$1 ORDER BY due_date,id", "tasks.xlsx"
	case "harvests":
		return "SELECT h.id,c.name crop_name,h.planting_id,h.field_id,h.quantity_kg,h.harvest_date,h.quality_grade,h.added_to_inventory_item_id,h.created_at FROM harvests h JOIN crops c ON c.id=h.crop_id WHERE h.farm_id=$1 ORDER BY h.id DESC", "harvests.xlsx"
	case "customers":
		return "SELECT id,name,type,phone,email,address,notes,created_at,updated_at FROM customers WHERE farm_id=$1 ORDER BY id", "customers.xlsx"
	case "sales":
		return "SELECT s.id,s.sale_date,c.name customer_name,i.name item_name,s.quantity_kg,s.price_per_kg,s.revenue_amount,s.cost_amount,s.profit_amount,s.status,s.created_at FROM sales s LEFT JOIN customers c ON c.id=s.customer_id LEFT JOIN inventory_items i ON i.id=s.inventory_item_id WHERE s.farm_id=$1 ORDER BY s.id DESC", "sales.xlsx"
	case "analytics":
		return "SELECT c.name crop_name, COALESCE(sum(s.revenue_amount),0) revenue, COALESCE(sum(s.profit_amount),0) profit FROM crops c LEFT JOIN sales s ON s.crop_id=c.id WHERE c.farm_id=$1 GROUP BY c.id,c.name ORDER BY profit DESC", "analytics.xlsx"
	default:
		return "", ""
	}
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
	emailSent := a.notifyEmail("AgroCRM: новый публичный заказ", []string{
		fmt.Sprintf("ID заказа: %d", id),
		"Тип: публичный заказ",
		"Клиент: " + trim(in.CustomerName, 160),
		"Телефон: " + trim(in.Phone, 40),
		"Email: " + trim(in.Email, 160),
		fmt.Sprintf("Общий вес: %.2f кг", totalQty),
		fmt.Sprintf("Оценочная сумма: %.2f ₽", amount),
		"Позиции: " + string(b),
	})
	c.JSON(201, gin.H{"id": id, "status": "new", "estimatedAmount": amount, "emailSent": emailSent})
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
	var id int
	if err := row.Scan(&id); err != nil {
		httpx.Error(c, 500, "DB_ERROR", "Ошибка заявки")
		return
	}
	emailSent := a.notifyEmail("AgroCRM: новый лид", []string{
		fmt.Sprintf("ID лида: %d", id),
		"Тип: публичный лид",
		"Имя: " + trim(in.Name, 160),
		"Телефон: " + trim(in.Phone, 40),
		"Email: " + trim(in.Email, 160),
		"Сообщение: " + trim(in.Message, 1000),
	})
	c.JSON(http.StatusCreated, gin.H{"id": id, "status": "new", "emailSent": emailSent})
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
	emailSent := a.notifyEmail("AgroCRM: новая заявка на доступ", []string{
		fmt.Sprintf("ID заявки: %d", id),
		"Тип: заявка на доступ",
		"ФИО: " + trim(in.OwnerName, 160),
		"Название фермы: " + trim(in.FarmName, 160),
		"Email: " + trim(in.Email, 160),
		"Телефон: " + trim(in.Phone, 40),
		"Площадь земли: " + trim(in.LandArea, 80),
		"Масштаб бизнеса: " + trim(in.BusinessScale, 80),
		"Регион: " + trim(in.Region, 120),
		"Комментарий: " + trim(in.Comment, 1000),
	})
	c.JSON(http.StatusCreated, gin.H{"id": id, "status": "new", "message": "Заявка принята. Мы свяжемся с вами и выдадим доступ.", "emailSent": emailSent})
}

func (a *API) notifyEmail(subject string, lines []string) bool {
	if a.mailer == nil || !a.mailer.Enabled() {
		return false
	}
	clean := []string{"Новая заявка из AgroCRM", ""}
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			clean = append(clean, line)
		}
	}
	clean = append(clean, "", "Откройте AgroCRM, чтобы обработать заявку.")
	if err := a.mailer.Send(subject, strings.Join(clean, "\n")); err != nil {
		log.Printf("email notification failed: %v", err)
		return false
	}
	return true
}

func (a *API) adminSummary(c *gin.Context) {
	var applications, leads, orders, newApplications, newLeads, newOrders int
	_ = a.store.DB.QueryRow("SELECT count(*) FROM access_applications").Scan(&applications)
	_ = a.store.DB.QueryRow("SELECT count(*) FROM external_orders WHERE source='lead'").Scan(&leads)
	_ = a.store.DB.QueryRow("SELECT count(*) FROM external_orders WHERE source<>'lead'").Scan(&orders)
	_ = a.store.DB.QueryRow("SELECT count(*) FROM access_applications WHERE status='new'").Scan(&newApplications)
	_ = a.store.DB.QueryRow("SELECT count(*) FROM external_orders WHERE source='lead' AND status='new'").Scan(&newLeads)
	_ = a.store.DB.QueryRow("SELECT count(*) FROM external_orders WHERE source<>'lead' AND status='new'").Scan(&newOrders)
	c.JSON(http.StatusOK, gin.H{
		"applications":    applications,
		"leads":           leads,
		"orders":          orders,
		"newApplications": newApplications,
		"newLeads":        newLeads,
		"newOrders":       newOrders,
	})
}

func (a *API) adminApplications(c *gin.Context) {
	items, err := a.store.Rows("SELECT id,owner_name,farm_name,email,phone,land_area,business_scale,region,comment,status,created_at,updated_at FROM access_applications ORDER BY created_at DESC, id DESC")
	if err != nil {
		httpx.Error(c, 500, "DB_ERROR", "Ошибка загрузки заявок")
		return
	}
	c.JSON(http.StatusOK, items)
}

func (a *API) adminOrders(c *gin.Context) {
	items, err := a.store.Rows("SELECT id,farm_id,customer_name,phone,email,source,status,items_json,total_quantity_kg,estimated_amount,created_at,updated_at FROM external_orders ORDER BY created_at DESC, id DESC")
	if err != nil {
		httpx.Error(c, 500, "DB_ERROR", "Ошибка загрузки обращений")
		return
	}
	c.JSON(http.StatusOK, items)
}

func (a *API) adminPatchApplication(c *gin.Context) {
	var in struct{ Status string }
	if !httpx.Bind(c, &in) {
		return
	}
	status := adminStatus(in.Status)
	if status == "" {
		httpx.Error(c, 422, "VALIDATION_ERROR", "Некорректный статус")
		return
	}
	id, ok := idParam(c, "applicationId")
	if ok {
		a.exec(c, "UPDATE access_applications SET status=$1, updated_at=now() WHERE id=$2", status, id)
	}
}

func (a *API) adminPatchOrder(c *gin.Context) {
	var in struct{ Status string }
	if !httpx.Bind(c, &in) {
		return
	}
	status := adminStatus(in.Status)
	if status == "" {
		httpx.Error(c, 422, "VALIDATION_ERROR", "Некорректный статус")
		return
	}
	id, ok := idParam(c, "orderId")
	if ok {
		a.exec(c, "UPDATE external_orders SET status=$1, updated_at=now() WHERE id=$2", status, id)
	}
}

func adminStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "new", "in_progress", "approved", "done", "rejected", "cancelled":
		return strings.ToLower(strings.TrimSpace(status))
	default:
		return ""
	}
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

func normalizeHeader(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	replacer := strings.NewReplacer(" ", "", "_", "", "-", "", "№", "", ".", "", "(", "", ")", "")
	return replacer.Replace(s)
}
func rowValue(row []string, headers map[string]int, keys ...string) string {
	for _, key := range keys {
		if idx, ok := headers[normalizeHeader(key)]; ok && idx < len(row) {
			return strings.TrimSpace(row[idx])
		}
	}
	return ""
}
func isRowEmpty(row []string) bool {
	for _, cell := range row {
		if strings.TrimSpace(cell) != "" {
			return false
		}
	}
	return true
}
func atoi(s string) int {
	v, _ := strconv.Atoi(strings.TrimSpace(s))
	return v
}
func atof(s string) float64 {
	s = strings.ReplaceAll(strings.TrimSpace(s), ",", ".")
	v, _ := strconv.ParseFloat(s, 64)
	return v
}
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
func stringify(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case time.Time:
		return x.Format(time.RFC3339)
	default:
		return fmt.Sprint(x)
	}
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
	if cached, ok := c.Get("json_body_map"); ok {
		if m, ok := cached.(map[string]any); ok {
			if v, ok := m[key]; ok {
				if s, ok := v.(string); ok {
					return trim(s, 500)
				}
			}
			return ""
		}
	}
	var m map[string]any
	if err := c.ShouldBindJSON(&m); err != nil {
		return ""
	}
	c.Set("json_body_map", m)
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return trim(s, 500)
		}
	}
	return ""
}
