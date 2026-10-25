package staff

type Staff struct {
	ID           string  `json:"id"`
	FullName     string  `json:"fullName"`
	Nickname     *string `json:"nickname"`
	Position     *string `json:"position"`
	Specialty    *string `json:"specialty"`
	Bio          *string `json:"bio"`
	Phone        *string `json:"phone"`
	AvatarURL    *string `json:"avatarUrl"`
	IsActive     bool    `json:"isActive"`
	IsBookable   bool    `json:"isBookable"`
	DisplayOrder int     `json:"displayOrder"`
}

type CreateStaffRequest struct {
	FullName     string  `json:"fullName" binding:"required,max=120"`
	Nickname     *string `json:"nickname" binding:"omitempty,max=60"`
	Position     *string `json:"position" binding:"omitempty,max=80"`
	Specialty    *string `json:"specialty" binding:"omitempty,max=160"`
	Bio          *string `json:"bio" binding:"omitempty,max=2000"`
	Phone        *string `json:"phone" binding:"omitempty,max=20"`
	AvatarURL    *string `json:"avatarUrl" binding:"omitempty,max=500"`
	IsActive     *bool   `json:"isActive"`
	IsBookable   *bool   `json:"isBookable"`
	DisplayOrder int     `json:"displayOrder" binding:"min=0,max=9999"`
}

type UpdateStaffRequest struct {
	FullName     *string `json:"fullName" binding:"omitempty,min=1,max=120"`
	Nickname     *string `json:"nickname" binding:"omitempty,max=60"`
	Position     *string `json:"position" binding:"omitempty,max=80"`
	Specialty    *string `json:"specialty" binding:"omitempty,max=160"`
	Bio          *string `json:"bio" binding:"omitempty,max=2000"`
	Phone        *string `json:"phone" binding:"omitempty,max=20"`
	AvatarURL    *string `json:"avatarUrl" binding:"omitempty,max=500"`
	IsActive     *bool   `json:"isActive"`
	IsBookable   *bool   `json:"isBookable"`
	DisplayOrder *int    `json:"displayOrder" binding:"omitempty,min=0,max=9999"`
}

type ScheduleEntry struct {
	Weekday int    `json:"weekday" binding:"min=0,max=6"`
	Start   string `json:"start" binding:"required"`
	End     string `json:"end" binding:"required"`
}

type ScheduleRequest struct {
	Entries []ScheduleEntry `json:"entries" binding:"max=7,dive"`
}

type TimeOff struct {
	ID        string `json:"id"`
	StaffID   string `json:"staffId"`
	StartDate string `json:"startDate"`
	EndDate   string `json:"endDate"`
	Reason    string `json:"reason"`
	Type      string `json:"type"`
}

type CreateTimeOffRequest struct {
	StartDate string `json:"startDate" binding:"required,datetime=2006-01-02"`
	EndDate   string `json:"endDate" binding:"required,datetime=2006-01-02"`
	Reason    string `json:"reason" binding:"required,min=3,max=200"`
	Type      string `json:"type" binding:"omitempty,oneof=leave holiday"`
}

type AttendanceRecord struct {
	ID        string  `json:"id"`
	StaffID   string  `json:"staffId"`
	StaffName string  `json:"staffName"`
	WorkDate  string  `json:"workDate"`
	ClockIn   *string `json:"clockIn"`
	ClockOut  *string `json:"clockOut"`
	Note      *string `json:"note"`
}

type ClockRequest struct {
	Note *string `json:"note" binding:"omitempty,max=200"`
}
