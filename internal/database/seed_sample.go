package database

import (
	"context"
	"log/slog"

	"github.com/jackc/pgx/v5/pgxpool"
)

func img(id string) string {
	return "https://images.unsplash.com/" + id + "?auto=format&fit=crop&w=1200&q=80"
}

// SeedSampleData เติมข้อมูลตัวอย่างสำหรับ development เท่านั้น
// แต่ละกลุ่มมีเงื่อนไขของตัวเอง — เติมเฉพาะเมื่อยังไม่มีข้อมูลในตารางนั้น
func SeedSampleData(ctx context.Context, pool *pgxpool.Pool) error {
	var serviceCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM services`).Scan(&serviceCount); err != nil {
		return err
	}
	if serviceCount == 0 {
		if err := seedStore(ctx, pool); err != nil {
			return err
		}
		if err := seedServices(ctx, pool); err != nil {
			return err
		}
		if err := seedStaff(ctx, pool); err != nil {
			return err
		}
		if err := seedGallery(ctx, pool); err != nil {
			return err
		}
		slog.Info("sample website data seeded (development only)")
	}

	var itemCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM inventory_items`).Scan(&itemCount); err != nil {
		return err
	}
	if itemCount == 0 {
		if err := seedInventory(ctx, pool); err != nil {
			return err
		}
		slog.Info("sample inventory seeded (development only)")
	}
	return nil
}

func seedStore(ctx context.Context, pool *pgxpool.Pool) error {
	tagline := "ครอบครัวช่างตัดผมสายคลาสสิก กลางใจเมือง — ตัดจริง แต่งจริง ใส่ใจทุกรายละเอียด"
	description := "The Barber House คือร้านตัดผมสำหรับคุณผู้ชายที่ให้ความสำคัญกับทรงผมและการดูแลตัวเอง ทีมช่างของเรามีประสบการณ์มากกว่า 10 ปี เชี่ยวชาญทั้งทรงคลาสสิก Skin Fade การแต่งหนวด และการโกนด้วยมีดอย่างร้านดั้งเดิม เราเลือกใช้ผลิตภัณฑ์คุณภาพดี พร้อมบริการ Hot Towel ทุกครั้งที่ตัดผม"
	address := "123 ซอยสุขุมวิท 31 แขวงคลองตันเหนือ เขตวัฒนา กรุงเทพมหานคร 10110"
	phone := "02-123-4567"
	email := "hello@thebarberhouse.example"
	hero := img("photo-1503951914875-452162b0f3f1")
	mapEmbed := "https://maps.google.com/maps?q=Sukhumvit%2031%20Bangkok&z=15&output=embed"
	social := `{"facebook":"https://www.facebook.com/thebarberhouse.example","instagram":"https://www.instagram.com/thebarberhouse.example","line":"https://line.me/R/ti/p/@thebarberhouse"}`
	hours := `{
		"1": {"open": "10:00", "close": "20:00", "closed": false},
		"2": {"open": "10:00", "close": "20:00", "closed": false},
		"3": {"open": "10:00", "close": "20:00", "closed": false},
		"4": {"open": "10:00", "close": "20:00", "closed": false},
		"5": {"open": "10:00", "close": "20:00", "closed": false},
		"6": {"open": "10:00", "close": "20:00", "closed": false},
		"0": {"open": "10:00", "close": "18:00", "closed": false}
	}`

	// เติมข้อมูลเฉพาะเมื่อยังเป็นค่า default (ยังไม่เคยแก้ไข)
	_, err := pool.Exec(ctx, `
		UPDATE store_settings SET
			store_name = 'The Barber House', tagline = $1, description = $2, hero_image_url = $3,
			address = $4, phone = $5, email = $6, map_embed_url = $7,
			social_links = $8::jsonb, opening_hours = $9::jsonb, updated_at = now()
		WHERE id = 1 AND store_name = 'Barbershop'`,
		tagline, description, hero, address, phone, email, mapEmbed, social, hours,
	)
	return err
}

func seedServices(ctx context.Context, pool *pgxpool.Pool) error {
	type sample struct {
		name, desc, price, image string
		minutes, order           int
		featured                 bool
	}
	list := []sample{
		{"Haircut", "ตัดผมชายตามทรงที่เหมาะกับรูปหน้า พร้อมสระผมและจัดทรงเบื้องต้น โดยช่างผู้เชี่ยวชาญ", "250", img("photo-1622286342621-4bd786c2447c"), 45, 1, true},
		{"Haircut + Beard", "ตัดผมพร้อมแต่งหนวดและโกนคมด้วย Hot Towel ครบจบในการจองครั้งเดียว", "350", img("photo-1599351431202-1e0f0137899a"), 60, 2, true},
		{"Hair Wash", "สระผมด้วยแชมพูอ่อนโยนพร้อมนวดศีรษะผ่อนคลาย ฟื้นฟูหนังศีรษะอย่างลึก", "150", img("photo-1570172619644-dfd03ed5d881"), 20, 3, false},
		{"Hair Styling", "จัดแต่งทรงด้วยความร้อนและผลิตภัณฑ์จัดแต่งทรง เหมาะกับโอกาสพิเศษหรือถ่ายภาพ", "200", img("photo-1560066984-138dadb4c035"), 30, 4, false},
		{"Beard Trim", "แต่งหนวดเฉพาะทรง โกนคมเนียน พร้อมบำรุงด้วยน้ำมันหนวด", "150", img("photo-1605497788044-5a32c7078486"), 30, 5, true},
		{"Hair Coloring", "ย้อมสีผมด้วยย้อมคุณภาพดี ลดความเสียหายต่อเส้นผม พร้อมปรึกษาสีที่เหมาะกับโทนผิว", "800", img("photo-1562322140-8baeececf3df"), 90, 6, false},
		{"Hair Treatment", "ฟื้นฟูเส้นผมที่เสียจากความร้อนหรือการย้อม ด้วยทรีตเมนต์เข้มข้นพร้อมนวดศีรษะ", "500", img("photo-1522337660859-02fbefca4702"), 60, 7, false},
	}
	for _, s := range list {
		if _, err := pool.Exec(ctx, `
			INSERT INTO services (name, slug, description, price, duration_minutes, image_url, is_active, is_featured, display_order)
			VALUES ($1, $2, $3, $4::numeric, $5, $6, true, $7, $8)`,
			s.name, generateSampleSlug(s.name), s.desc, s.price, s.minutes, s.image, s.featured, s.order,
		); err != nil {
			return err
		}
	}
	return nil
}

func seedStaff(ctx context.Context, pool *pgxpool.Pool) error {
	type sample struct {
		name, nickname, position, specialty, bio, avatar string
	}
	list := []sample{
		{"สมชาย ใจดี", "เชียร์", "Master Barber", "Classic Cut, Skin Fade, Hot Towel Shave", "เชียร์ทำงานในวงการตัดผมมากว่า 15 ปี เคยอยู่ร้านดังในสีลมและธงษีวาร ถนัดทรงคลาสสิกย้อนยุคและ Skin Fade คมกริบ", img("photo-1507003211169-0a1dd7228f2d")},
		{"อนุชา วงศ์ดี", "เบียร์", "Senior Barber", "Beard Sculpting, Razor Work", "เบียร์คือมือขวาเรื่องการแต่งหนวดและการโกนด้วยมีด เชื่อว่าหนวดที่ดีต้องเริ่มจากการสื่อสารกับลูกค้าให้เข้าใจทรงที่ต้องการ", img("photo-1500648767791-00dcc994a43e")},
		{"ธนพล ศรีสุข", "ต้า", "Barber", "Hair Styling, Kids Haircut", "ต้าเชี่ยวชาญการจัดแต่งทรงด้วยความร้อนและทรงผมสำหรับเด็ก ใจเย็นและอดทนเป็นพิเศษกับลูกค้าตัวเล็ก", img("photo-1506794778202-cad84cf45f1d")},
		{"กิตติภัทร ทองมา", "กอล์ฟ", "Barber", "Hair Coloring, Hair Treatment", "กอล์ฟดูแลเรื่องสีผมและการฟื้นฟูเส้นผม อัปเดตเทรนด์สีใหม่ ๆ ตลอดเวลาเพื่อให้ลูกค้าได้สีที่ใช่", img("photo-1472099645785-5658abf4ff4e")},
	}
	for i, s := range list {
		var staffID string
		err := pool.QueryRow(ctx, `
			INSERT INTO staff (full_name, nickname, position, specialty, bio, avatar_url, is_active, is_bookable, display_order)
			VALUES ($1, $2, $3, $4, $5, $6, true, true, $7) RETURNING id`,
			s.name, s.nickname, s.position, s.specialty, s.bio, s.avatar, i+1,
		).Scan(&staffID)
		if err != nil {
			return err
		}
		// ตารางงานจันทร์-เสาร์ 10:00-20:00 (เวลาเปิดร้านของข้อมูลตัวอย่าง)
		for weekday := 1; weekday <= 6; weekday++ {
			if _, err := pool.Exec(ctx, `
				INSERT INTO staff_schedules (staff_id, weekday, start_time, end_time) VALUES ($1, $2, '10:00', '20:00')`,
				staffID, weekday,
			); err != nil {
				return err
			}
		}
	}
	return nil
}

func seedGallery(ctx context.Context, pool *pgxpool.Pool) error {
	type sample struct{ image, caption string }
	list := []sample{
		{img("photo-1503951914875-452162b0f3f1"), "บรรยากาศการตัดผมในร้าน"},
		{img("photo-1585747860715-2ba37e788b70"), "มุมโปรดของร้าน"},
		{img("photo-1521590832167-7bcbfaa6381f"), "งานตัดผมของเชียร์"},
		{img("photo-1621605815971-fbc98d665033"), "Skin Fade คมกริบ"},
		{img("photo-1605497788044-5a32c7078486"), "งานโกนด้วยมีด"},
		{img("photo-1599351431202-1e0f0137899a"), "แต่งหนวดเฉพาะทรง"},
		{img("photo-1493256338651-d82f7acb2b38"), "เครื่องมือที่เราใช้ทุกวัน"},
		{img("photo-1622286342621-4bd786c2447c"), "ทรงผมที่ลูกค้าถูกใจ"},
	}
	for i, g := range list {
		if _, err := pool.Exec(ctx, `
			INSERT INTO gallery_images (image_url, caption, display_order, is_active) VALUES ($1, $2, $3, true)`,
			g.image, g.caption, i+1,
		); err != nil {
			return err
		}
	}
	return nil
}

func generateSampleSlug(name string) string {
	switch name {
	case "Haircut":
		return "haircut"
	case "Haircut + Beard":
		return "haircut-beard"
	case "Hair Wash":
		return "hair-wash"
	case "Hair Styling":
		return "hair-styling"
	case "Beard Trim":
		return "beard-trim"
	case "Hair Coloring":
		return "hair-coloring"
	case "Hair Treatment":
		return "hair-treatment"
	}
	return name
}
