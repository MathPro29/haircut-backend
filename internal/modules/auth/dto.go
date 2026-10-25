package auth

type LoginRequest struct {
	Username string `json:"username" binding:"required,max=60"`
	Password string `json:"password" binding:"required,min=6,max=100"`
}

type AuthUser struct {
	ID       string  `json:"id"`
	Username string  `json:"username"`
	FullName string  `json:"fullName"`
	Role     string  `json:"role"`
	StaffID  *string `json:"staffId"`
}
