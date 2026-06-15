# AgroCRM

Небольшая CRM для фермерского хозяйства: поля, посевы, склад, задачи, урожай, продажи, аналитика и публичный API.

## Стек

- Frontend: React + Vite
- Backend: Go + Gin
- Database: PostgreSQL
- Запуск: Docker Compose

## Быстро развернуть

Нужны Docker и Docker Compose.

```bash
git clone https://github.com/Mountok/AgroCRM.git
cd AgroCRM
docker compose up --build
```

После запуска:

- Frontend: http://localhost:5173
- Backend health: http://localhost:8080/health
- Public products: http://localhost:8080/public/products

Backend сам создаёт таблицы и демо-данные при старте.

Если нужно получать заявки на почту, заполните SMTP-переменные из `.env.example`
и запустите Docker Compose с этим `.env` файлом. Без SMTP заявки всё равно
сохраняются в базе в таблицу `access_applications`.

## Демо-сценарий

1. Открыть Dashboard.
2. Создать посев картофеля — со склада спишется 500 кг семян.
3. Проверить склад и задачи.
4. Добавить урожай 5800 кг.
5. Создать продажу 1000 кг по 35 ₽.
6. Открыть аналитику и публичный API.

## Локальный запуск без Docker

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
