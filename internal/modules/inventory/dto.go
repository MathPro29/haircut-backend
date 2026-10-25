package inventory

type Category struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Item struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	CategoryID   *string `json:"categoryId"`
	CategoryName *string `json:"categoryName"`
	Unit         string  `json:"unit"`
	Quantity     int     `json:"quantity"`
	MinQuantity  int     `json:"minQuantity"`
	CostPrice    *string `json:"costPrice,omitempty"` // แสดงเฉพาะ owner/manager
	ImageURL     *string `json:"imageUrl"`
	Barcode      *string `json:"barcode"`
	IsActive     bool    `json:"isActive"`
	Note         *string `json:"note"`
}

type CreateItemRequest struct {
	Name        string  `json:"name" binding:"required,max=140"`
	CategoryID  *string `json:"categoryId" binding:"omitempty,uuid"`
	Unit        string  `json:"unit" binding:"required,oneof=piece bottle jar tube box other"`
	Quantity    int     `json:"quantity" binding:"min=0,max=1000000"`
	MinQuantity int     `json:"minQuantity" binding:"min=0,max=1000000"`
	CostPrice   *string `json:"costPrice" binding:"omitempty,max=20"`
	ImageURL    *string `json:"imageUrl" binding:"omitempty,max=500"`
	Barcode     *string `json:"barcode" binding:"omitempty,max=64"`
	Note        *string `json:"note" binding:"omitempty,max=300"`
	IsActive    *bool   `json:"isActive"`
}

// UpdateItemRequest แก้ได้เฉพาะข้อมูลรายการ — จำนวนสต็อกต้องเปลี่ยนผ่าน movement เท่านั้น
type UpdateItemRequest struct {
	Name        *string `json:"name" binding:"omitempty,min=1,max=140"`
	CategoryID  *string `json:"categoryId" binding:"omitempty,uuid"`
	Unit        *string `json:"unit" binding:"omitempty,oneof=piece bottle jar tube box other"`
	MinQuantity *int    `json:"minQuantity" binding:"omitempty,min=0,max=1000000"`
	CostPrice   *string `json:"costPrice" binding:"omitempty,max=20"`
	ImageURL    *string `json:"imageUrl" binding:"omitempty,max=500"`
	Barcode     *string `json:"barcode" binding:"omitempty,max=64"`
	Note        *string `json:"note" binding:"omitempty,max=300"`
	IsActive    *bool   `json:"isActive"`
}

type MovementRequest struct {
	ItemID   string `json:"itemId" binding:"required,uuid"`
	Type     string `json:"type" binding:"required,oneof=in out adjustment damage"`
	Quantity int    `json:"quantity" binding:"required,min=1,max=1000000"`
	Reason   string `json:"reason" binding:"required,min=3,max=300"`
}

type MovementRecord struct {
	ID              string  `json:"id"`
	ItemID          string  `json:"itemId"`
	Type            string  `json:"type"`
	QuantityChange  int     `json:"quantityChange"`
	BalanceAfter    int     `json:"balanceAfter"`
	Reason          string  `json:"reason"`
	PerformedByName *string `json:"performedByName"`
	CreatedAt       string  `json:"createdAt"`
}

type CategoryRequest struct {
	Name string `json:"name" binding:"required,min=1,max=80"`
}
