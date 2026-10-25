package services

type Service struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	Slug            string  `json:"slug"`
	Description     *string `json:"description"`
	Price           string  `json:"price"`
	DurationMinutes int     `json:"durationMinutes"`
	ImageURL        *string `json:"imageUrl"`
	IsActive        bool    `json:"isActive"`
	IsFeatured      bool    `json:"isFeatured"`
	DisplayOrder    int     `json:"displayOrder"`
}

type CreateRequest struct {
	Name            string  `json:"name" binding:"required,max=120"`
	Slug            string  `json:"slug" binding:"omitempty,max=140"`
	Description     *string `json:"description" binding:"omitempty,max=1000"`
	Price           string  `json:"price" binding:"required,max=20"`
	DurationMinutes int     `json:"durationMinutes" binding:"required,min=5,max=480"`
	ImageURL        *string `json:"imageUrl" binding:"omitempty,max=500"`
	IsActive        *bool   `json:"isActive"`
	IsFeatured      bool    `json:"isFeatured"`
	DisplayOrder    int     `json:"displayOrder" binding:"min=0,max=9999"`
}

// UpdateRequest ทุก field เป็น pointer — ส่งเฉพาะ field ที่ต้องการแก้ (string ว่าง = ล้างค่า)
type UpdateRequest struct {
	Name            *string `json:"name" binding:"omitempty,min=1,max=120"`
	Slug            *string `json:"slug" binding:"omitempty,max=140"`
	Description     *string `json:"description" binding:"omitempty,max=1000"`
	Price           *string `json:"price" binding:"omitempty,max=20"`
	DurationMinutes *int    `json:"durationMinutes" binding:"omitempty,min=5,max=480"`
	ImageURL        *string `json:"imageUrl" binding:"omitempty,max=500"`
	IsActive        *bool   `json:"isActive"`
	IsFeatured      *bool   `json:"isFeatured"`
	DisplayOrder    *int    `json:"displayOrder" binding:"omitempty,min=0,max=9999"`
}
