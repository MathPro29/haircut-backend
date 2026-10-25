CREATE EXTENSION IF NOT EXISTS btree_gist;

CREATE TYPE user_role AS ENUM ('owner', 'manager', 'staff');
CREATE TYPE booking_status AS ENUM ('pending', 'confirmed', 'completed', 'cancelled', 'no_show');
CREATE TYPE booking_source AS ENUM ('public', 'admin');
CREATE TYPE movement_type AS ENUM ('in', 'out', 'adjustment', 'damage');
CREATE TYPE time_off_type AS ENUM ('leave', 'holiday');

-- ข้อมูลช่าง/พนักงาน (แสดงบนเว็บหน้าบ้าน แยกจากบัญชีเข้าสู่ระบบ)
CREATE TABLE staff (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    full_name VARCHAR(120) NOT NULL,
    nickname VARCHAR(60),
    position VARCHAR(80),
    specialty VARCHAR(160),
    bio TEXT,
    phone VARCHAR(20),
    avatar_url TEXT,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    is_bookable BOOLEAN NOT NULL DEFAULT TRUE,
    display_order INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- บัญชีเข้าสู่ระบบหลังบ้าน
CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username VARCHAR(60) NOT NULL UNIQUE,
    password_hash VARCHAR(100) NOT NULL,
    full_name VARCHAR(120) NOT NULL,
    role user_role NOT NULL DEFAULT 'staff',
    staff_id UUID REFERENCES staff(id) ON DELETE SET NULL,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    last_login_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_users_staff ON users (staff_id);

-- ตารางงานรายสัปดาห์ (recurring)
CREATE TABLE staff_schedules (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    staff_id UUID NOT NULL REFERENCES staff(id) ON DELETE CASCADE,
    weekday SMALLINT NOT NULL CHECK (weekday BETWEEN 0 AND 6),
    start_time TIME NOT NULL,
    end_time TIME NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (end_time > start_time),
    UNIQUE (staff_id, weekday)
);

-- วันหยุด/วันลาเป็นช่วงวันที่เฉพาะเจาะจง
CREATE TABLE staff_time_off (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    staff_id UUID NOT NULL REFERENCES staff(id) ON DELETE CASCADE,
    start_date DATE NOT NULL,
    end_date DATE NOT NULL,
    reason VARCHAR(200) NOT NULL,
    type time_off_type NOT NULL DEFAULT 'leave',
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (end_date >= start_date)
);

-- ลงเวลาเข้า-ออกงาน (Phase 1: กดบันทึกผ่านบัญชี วันละ 1 รายการ)
CREATE TABLE attendance_records (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    staff_id UUID NOT NULL REFERENCES staff(id) ON DELETE CASCADE,
    work_date DATE NOT NULL,
    clock_in_at TIMESTAMPTZ NOT NULL,
    clock_out_at TIMESTAMPTZ,
    note VARCHAR(200),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (staff_id, work_date)
);

CREATE TABLE services (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(120) NOT NULL,
    slug VARCHAR(140) NOT NULL UNIQUE,
    description TEXT,
    price NUMERIC(10,2) NOT NULL CHECK (price >= 0),
    duration_minutes INT NOT NULL CHECK (duration_minutes > 0),
    image_url TEXT,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    is_featured BOOLEAN NOT NULL DEFAULT FALSE,
    display_order INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- การจองคิว: staff_id ต้องมีเสมอ (ลูกค้าไม่ระบุ = ระบบ auto-assign ช่างที่อยู่ในกะ)
CREATE TABLE bookings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code VARCHAR(12) NOT NULL UNIQUE,
    customer_name VARCHAR(120) NOT NULL,
    customer_phone VARCHAR(20) NOT NULL,
    note VARCHAR(500),
    staff_id UUID NOT NULL REFERENCES staff(id) ON DELETE RESTRICT,
    start_time TIMESTAMPTZ NOT NULL,
    end_time TIMESTAMPTZ NOT NULL,
    status booking_status NOT NULL DEFAULT 'pending',
    source booking_source NOT NULL DEFAULT 'public',
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    cancelled_reason VARCHAR(300),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (end_time > start_time)
);

-- กันจองซ้ำ/ทับคิวของช่างคนเดียวกัน (ปลอดภัยต่อ concurrent requests)
ALTER TABLE bookings ADD CONSTRAINT no_overlapping_active_booking
    EXCLUDE USING gist (
        staff_id WITH =,
        tstzrange(start_time, end_time) WITH &&
    ) WHERE (status IN ('pending', 'confirmed'));

CREATE INDEX idx_bookings_staff_start ON bookings (staff_id, start_time);
CREATE INDEX idx_bookings_start ON bookings (start_time);
CREATE INDEX idx_bookings_status ON bookings (status);
CREATE INDEX idx_bookings_phone ON bookings (customer_phone);

-- 1 การจองมีได้หลายบริการ เก็บ snapshot ราคา/เวลาตอนจอง
CREATE TABLE booking_services (
    booking_id UUID NOT NULL REFERENCES bookings(id) ON DELETE CASCADE,
    service_id UUID NOT NULL REFERENCES services(id) ON DELETE RESTRICT,
    service_name VARCHAR(120) NOT NULL,
    price NUMERIC(10,2) NOT NULL,
    duration_minutes INT NOT NULL,
    PRIMARY KEY (booking_id, service_id)
);

CREATE TABLE inventory_categories (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(80) NOT NULL UNIQUE
);

-- Phase 1 นับสต็อกเป็นหน่วย (ชิ้น/ขวด/กระปุก) ยังไม่รองรับ ml/g
CREATE TABLE inventory_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    category_id UUID REFERENCES inventory_categories(id) ON DELETE SET NULL,
    name VARCHAR(140) NOT NULL,
    unit VARCHAR(20) NOT NULL DEFAULT 'piece',
    quantity INT NOT NULL DEFAULT 0 CHECK (quantity >= 0),
    min_quantity INT NOT NULL DEFAULT 0 CHECK (min_quantity >= 0),
    cost_price NUMERIC(10,2) CHECK (cost_price IS NULL OR cost_price >= 0),
    image_url TEXT,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    note VARCHAR(300),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_inventory_items_category ON inventory_items (category_id);

-- ทุกการเปลี่ยนแปลงสต็อกต้องมีรายการในตารางนี้ (ห้ามแก้ quantity ตรง ๆ)
CREATE TABLE stock_movements (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    item_id UUID NOT NULL REFERENCES inventory_items(id) ON DELETE CASCADE,
    type movement_type NOT NULL,
    quantity_change INT NOT NULL CHECK (quantity_change <> 0),
    balance_after INT NOT NULL CHECK (balance_after >= 0),
    reason VARCHAR(300) NOT NULL,
    performed_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_stock_movements_item ON stock_movements (item_id, created_at);

-- ข้อมูลร้าน 1 แถวเดียว (single store)
CREATE TABLE store_settings (
    id INT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    store_name VARCHAR(120) NOT NULL DEFAULT 'Barbershop',
    tagline VARCHAR(200),
    description TEXT,
    logo_url TEXT,
    hero_image_url TEXT,
    address VARCHAR(300),
    phone VARCHAR(20),
    email VARCHAR(120),
    map_embed_url TEXT,
    social_links JSONB NOT NULL DEFAULT '{}',
    opening_hours JSONB NOT NULL DEFAULT '{}',
    max_advance_days INT NOT NULL DEFAULT 30 CHECK (max_advance_days BETWEEN 1 AND 365),
    auto_confirm_bookings BOOLEAN NOT NULL DEFAULT TRUE,
    cancellation_cutoff_minutes INT NOT NULL DEFAULT 60 CHECK (cancellation_cutoff_minutes >= 0),
    timezone VARCHAR(40) NOT NULL DEFAULT 'Asia/Bangkok',
    updated_by UUID REFERENCES users(id) ON DELETE SET NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO store_settings (id) VALUES (1);

-- วันปิดทำการพิเศษ
CREATE TABLE store_closures (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    closure_date DATE NOT NULL UNIQUE,
    reason VARCHAR(200)
);

CREATE TABLE gallery_images (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    image_url TEXT NOT NULL,
    caption VARCHAR(200),
    display_order INT NOT NULL DEFAULT 0,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE audit_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    action VARCHAR(60) NOT NULL,
    entity_type VARCHAR(40) NOT NULL,
    entity_id VARCHAR(64),
    changes JSONB,
    ip VARCHAR(45),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_audit_logs_entity ON audit_logs (entity_type, entity_id);
CREATE INDEX idx_audit_logs_created ON audit_logs (created_at);
