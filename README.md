# Barbershop Backend

ระบบหลังบ้าน + API ของร้านตัดผม — Go + Gin + PostgreSQL (Modular Monolith)

> เป็นส่วนหนึ่งของระบบเต็ม: Frontend อยู่ที่ repo [haircut-frontend](https://github.com/MathPro29/haircut-frontend)

## เริ่มต้น (Development)

ข้อกำหนด: Go 1.25+, Docker

```bash
cp .env.example .env          # แล้วแก้ค่าตามต้องการ
docker compose up -d          # PostgreSQL 16 (port 5432)
go run ./cmd/server           # API ที่ :8080 — migrations รันอัตโนมัติทุกครั้งที่ start
```

- บัญชี owner แรกสร้างอัตโนมัติจากค่า `OWNER_USERNAME` / `OWNER_PASSWORD` ใน `.env` (เฉพาะเมื่อยังไม่มีผู้ใช้)
- ข้อมูลตัวอย่าง (บริการ ช่าง แกลเลอรี สต็อก) seed อัตโนมัติเฉพาะ environment `dev` เมื่อตารางยังว่าง

## การทดสอบ

```bash
go test ./internal/modules/bookings/    # availability + กันจองซ้ำแบบ concurrent
go test ./internal/modules/inventory/   # กันสต็อกติดลบ + ยอดตรงประวัติ
```

(ต้องมี PostgreSQL รันอยู่จาก docker compose)

## โครงสร้าง

```
cmd/server/       entrypoint
internal/
  config/         อ่านค่าจาก env
  database/       pgxpool + migrations + seed
  httpx/          response/error format กลาง
  middleware/     JWT auth, RBAC, CORS, rate limit
  token/          JWT access/refresh
  modules/        auth, services, bookings, inventory, staff, settings, dashboard
migrations/       SQL migrations (embedded)
```

ดูแผนงานทั้งระบบและรายละเอียด API ในเอกสารโปรเจกต์
