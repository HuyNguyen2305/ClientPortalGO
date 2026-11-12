package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"clientportal/internal/dto"
	"clientportal/internal/middleware"
	"clientportal/internal/model"
	"clientportal/internal/service"
)

type bookingHandler struct {
	svc *service.BookingService
}

func (h bookingHandler) days(c *gin.Context) {
	days, prev, next, err := h.svc.Days(c.Query("from"))
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, dto.FromDays(days, prev, next))
}

func (h bookingHandler) slots(c *gin.Context) {
	id, err := PathID(c)
	if err != nil {
		RespondError(c, err)
		return
	}
	day, slots, err := h.svc.Slots(c.Request.Context(), id, c.Query("date"))
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, dto.SlotsResponse{Date: day.Format(time.DateOnly), Slots: slots})
}

func (h bookingHandler) create(c *gin.Context) {
	var req dto.BookingRequest
	if err := bindJSON(c, &req); err != nil {
		RespondError(c, err)
		return
	}
	b, err := h.svc.Book(c.Request.Context(), middleware.CustomerID(c), req.ToInput())
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, dto.FromBooking(b, h.svc.Location()))
}

func (h bookingHandler) list(c *gin.Context) {
	bs, err := h.svc.List(c.Request.Context(), middleware.CustomerID(c))
	if err != nil {
		RespondError(c, err)
		return
	}
	loc := h.svc.Location()
	c.JSON(http.StatusOK, dto.Page[dto.BookingResponse]{
		Data: dto.MapSlice(bs, func(b model.Booking) dto.BookingResponse { return dto.FromBooking(b, loc) }),
	})
}

func (h bookingHandler) get(c *gin.Context) {
	id, err := PathID(c)
	if err != nil {
		RespondError(c, err)
		return
	}
	b, err := h.svc.Get(c.Request.Context(), middleware.CustomerID(c), id)
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, dto.FromBooking(b, h.svc.Location()))
}
