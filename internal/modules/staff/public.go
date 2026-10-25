package staff

import (
	"github.com/gin-gonic/gin"

	"barbershop/internal/httpx"
)

type Barber struct {
	ID         string  `json:"id"`
	FullName   string  `json:"fullName"`
	Nickname   *string `json:"nickname"`
	Position   *string `json:"position"`
	Specialty  *string `json:"specialty"`
	Bio        *string `json:"bio"`
	AvatarURL  *string `json:"avatarUrl"`
	IsBookable bool    `json:"isBookable"`
}

// RegisterPublicRoutes endpoint สาหัวบ้านสำหรับหน้าเว็บ (ไม่ต้อง auth)
func (h *Handler) RegisterPublicRoutes(api *gin.RouterGroup) {
	api.GET("/public/barbers", h.listBarbers)
}

func (h *Handler) listBarbers(c *gin.Context) {
	list, err := h.mgr.List(c.Request.Context(), false)
	if err != nil {
		httpx.Err(c, err)
		return
	}
	barbers := make([]Barber, 0, len(list))
	for _, s := range list {
		barbers = append(barbers, Barber{
			ID: s.ID, FullName: s.FullName, Nickname: s.Nickname, Position: s.Position,
			Specialty: s.Specialty, Bio: s.Bio, AvatarURL: s.AvatarURL, IsBookable: s.IsBookable,
		})
	}
	httpx.OK(c, barbers)
}
