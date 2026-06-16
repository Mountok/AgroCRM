# AgroCRM

AgroCRM — MVP CRM-системы для фермерского хозяйства: поля, посевы, склад, задачи, урожай, клиенты, продажи, аналитика, публичное API, email-уведомления и простая админка для обработки заявок.

## Стек

- **Frontend:** React + JavaScript + Vite
- **Backend:** Go + Gin
- **Database:** PostgreSQL
- **Infra:** Docker Compose
- **Email:** SMTP / Gmail app password

## Быстрый запуск через Docker Compose

Нужны Docker и Docker Compose.

```bash
git clone https://github.com/Mountok/AgroCRM.git
cd AgroCRM
cp .env.example .env
docker compose up --build
```

После запуска:

- Frontend: http://localhost:5173
- Backend health: http://localhost:8080/health
- Swagger UI: http://localhost:8080/swagger
- Public products API: http://localhost:8080/public/products

Backend сам создаёт таблицы и демо-данные при старте.

## Минимальный `.env` для запуска

Для базового запуска можно оставить только SMTP-переменные. Остальное уже задано в `docker-compose.yml`.

```env
SMTP_HOST=smtp.gmail.com
SMTP_PORT=587
SMTP_USER=your-email@gmail.com
SMTP_PASSWORD=your-gmail-app-password
APPLICATIONS_EMAIL=your-email@gmail.com
```

Если email-уведомления не нужны, можно оставить `SMTP_PASSWORD` пустым. Заявки всё равно будут сохраняться в базе и отображаться в админке.

Важно: используйте **пароль приложения Gmail**, а не обычный пароль от почты.

## Полный пример `.env`

```env
DATABASE_URL=postgres://agrocrm:agrocrm@localhost:5432/agrocrm?sslmode=disable
FRONTEND_ORIGIN=http://localhost:5173,http://127.0.0.1:5173,http://0.0.0.0:5173
VITE_API_URL=http://localhost:8080

SMTP_HOST=smtp.gmail.com
SMTP_PORT=587
SMTP_USER=your-email@gmail.com
SMTP_PASSWORD=your-gmail-app-password
APPLICATIONS_EMAIL=your-email@gmail.com
```

Файл `.env` находится в `.gitignore` и не должен попадать в репозиторий.

## Доступы для демо

### Фермерская CRM

```text
Логин: demo
Пароль: demo
```

или:

```text
Логин: akhmat@example.com
Пароль: demo123
```

### Админка

```text
Логин: admin
Пароль: admin
```

Админка открывается автоматически после входа под ролью `admin`.

## Основной демо-сценарий

1. Открыть http://localhost:5173.
2. Показать заявку на доступ.
3. Войти в админку: `admin / admin`.
4. Показать заявки, лиды и публичные заказы.
5. Войти в CRM: `demo / demo`.
6. Открыть Dashboard.
7. Перейти на склад и показать остаток семян картофеля.
8. Создать посев картофеля на 500 кг семян.
9. Показать списание семян со склада.
10. Показать автоматически созданные задачи.
11. Добавить урожай.
12. Показать, что урожай попал на склад.
13. Создать клиента и продажу.
14. Показать прибыль в Dashboard/Analytics.
15. Открыть Swagger: http://localhost:8080/swagger.

## Главные возможности

- Авторизация и заявки на доступ.
- Отдельная админка для обработки заявок.
- Email-уведомления о заявках/лидах/заказах.
- Управление фермой и профилем.
- Поля, культуры и посевы.
- Автоматическое списание семян при посеве.
- Автоматические задачи после создания посева.
- Склад семян, урожая и прочих товаров.
- Урожай с автоматическим зачислением на склад.
- Клиенты и продажи.
- Расчёт выручки и прибыли.
- Dashboard и аналитика.
- XLSX import/export.
- Swagger/OpenAPI документация.
- Публичное API для продуктов, заказов, лидов и заявок.

## Публичное API

```http
GET  /public/products
POST /public/orders
POST /public/leads
POST /public/applications
```

## Admin API

```http
GET   /admin/summary
GET   /admin/applications
PATCH /admin/applications/:applicationId
GET   /admin/orders
PATCH /admin/orders/:orderId
```

## Локальный запуск без Docker

Нужно поднять PostgreSQL и указать `DATABASE_URL`.

Backend:

```bash
cd backend
go mod download
go run ./cmd/server
```

Frontend:

```bash
cd frontend
npm install
npm run dev
```

## Полезные команды

```bash
# Полный запуск
docker compose up --build

# Перезапустить только backend
docker compose up -d --build backend

# Перезапустить только frontend
docker compose up -d --build frontend

# Проверить backend
curl http://localhost:8080/health

# Открыть Swagger
open http://localhost:8080/swagger
```
