package bookings

type AvailabilityResponse struct {
	Date     string   `json:"date"`
	Timezone string   `json:"timezone"`
	Slots    []string `json:"slots"`
}

type CreateBookingRequest struct {
	ServiceIDs    []string `json:"serviceIds" binding:"required,min=1,max=5,dive,required"`
	StaffID       *string  `json:"staffId" binding:"omitempty,uuid"`
	Date          string   `json:"date" binding:"required"`
	StartTime     string   `json:"startTime" binding:"required"`
	CustomerName  string   `json:"customerName" binding:"required,min=2,max=120"`
	CustomerPhone string   `json:"customerPhone" binding:"required,min=8,max=20"`
	Note          *string  `json:"note" binding:"omitempty,max=500"`
	// Status ใช้เฉพาะ admin (ระบุ pending/confirmed ได้) — public ใช้ค่าจาก setting auto_confirm
	Status *string `json:"status" binding:"omitempty,oneof=pending confirmed"`
}

type BookingServiceItem struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Price           string `json:"price"`
	DurationMinutes int    `json:"durationMinutes"`
}

type BookingDetail struct {
	ID              string              `json:"id"`
	Code            string              `json:"code"`
	CustomerName    string              `json:"customerName"`
	CustomerPhone   string              `json:"customerPhone"`
	Note            *string             `json:"note"`
	StaffID         string              `json:"staffId"`
	StaffName       string              `json:"staffName"`
	StaffNickname   *string             `json:"staffNickname"`
	StartTime       string              `json:"startTime"` // ISO 8601 UTC
	EndTime         string              `json:"endTime"`
	Timezone        string              `json:"timezone"`
	Status          string              `json:"status"`
	Source          string              `json:"source"`
	Services        []BookingServiceItem `json:"services"`
	TotalPrice      string              `json:"totalPrice"`
	TotalDurationMinutes int            `json:"totalDurationMinutes"`
	CancelledReason *string             `json:"cancelledReason"`
	CreatedAt       string              `json:"createdAt"`
	// CanCancel — เฉพาะ lookup ของลูกค้า
	CanCancel bool `json:"canCancel,omitempty"`
}

type BookingSummary struct {
	ID             string   `json:"id"`
	Code           string   `json:"code"`
	CustomerName   string   `json:"customerName"`
	CustomerPhone  string   `json:"customerPhone"`
	StaffName      string   `json:"staffName"`
	StartTime      string   `json:"startTime"`
	EndTime        string   `json:"endTime"`
	Status         string   `json:"status"`
	Source         string   `json:"source"`
	ServiceNames   []string `json:"serviceNames"`
	TotalPrice     string   `json:"totalPrice"`
	Note           *string  `json:"note"`
}

type ListBookingsQuery struct {
	Search   string
	StaffID  string
	Status   string
	DateFrom string
	DateTo   string
	Page     int
	Limit    int
}

type UpdateStatusRequest struct {
	Status string `json:"status" binding:"required,oneof=confirmed completed no_show"`
}

type RescheduleRequest struct {
	Date      string  `json:"date" binding:"required"`
	StartTime string  `json:"startTime" binding:"required"`
	StaffID   *string `json:"staffId" binding:"omitempty,uuid"`
}

type CancelRequest struct {
	Reason string `json:"reason" binding:"omitempty,max=300"`
}

type CancelByCustomerRequest struct {
	Phone string `json:"phone" binding:"required"`
}
