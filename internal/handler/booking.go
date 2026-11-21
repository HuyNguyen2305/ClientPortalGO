package handler

import (
	"net/http"
	"strconv"
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

func (h bookingHandler) createSeries(c *gin.Context) {
	var req dto.BookingRequest
	if err := bindJSON(c, &req); err != nil {
		RespondError(c, err)
		return
	}
	s, err := h.svc.BookSeries(c.Request.Context(), middleware.CustomerID(c), req.ToInput())
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, dto.FromSeries(s, h.svc.Location()))
}

func (h bookingHandler) cancel(c *gin.Context) {
	id, err := PathID(c)
	if err != nil {
		RespondError(c, err)
		return
	}
	b, err := h.svc.Cancel(c.Request.Context(), middleware.CustomerID(c), id)
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, dto.FromBooking(b, h.svc.Location()))
}

func (h bookingHandler) start(c *gin.Context) {
	id, err := PathID(c)
	if err != nil {
		RespondError(c, err)
		return
	}
	if err := h.svc.Start(c.Request.Context(), id); err != nil {
		RespondError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h bookingHandler) complete(c *gin.Context) {
	id, err := PathID(c)
	if err != nil {
		RespondError(c, err)
		return
	}
	var req dto.CompleteRequest
	if err := bindJSON(c, &req); err != nil {
		RespondError(c, err)
		return
	}
	if err := h.svc.Complete(c.Request.Context(), id, req.Notes); err != nil {
		RespondError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// listWork pages through the customer's work orders; each item is its booking with the work order inside.
func (h bookingHandler) listWork(c *gin.Context) {
	limit, err := dto.ParseLimit(c.Query("limit"))
	if err != nil {
		RespondError(c, err)
		return
	}
	f := model.WorkFilter{CustomerID: middleware.CustomerID(c), Status: model.WorkStatus(c.Query("status")), Limit: limit}
	if raw := c.Query("cursor"); raw != "" {
		if f.AfterID, err = positiveInt64("cursor", raw); err != nil {
			RespondError(c, err)
			return
		}
	}
	if raw := c.Query("service_id"); raw != "" {
		if f.ServiceID, err = positiveInt64("service_id", raw); err != nil {
			RespondError(c, err)
			return
		}
	}
	items, next, err := h.svc.ListWork(c.Request.Context(), f)
	if err != nil {
		RespondError(c, err)
		return
	}
	loc := h.svc.Location()
	page := dto.Page[dto.BookingResponse]{
		Data: dto.MapSlice(items, func(b model.Booking) dto.BookingResponse { return dto.FromBooking(b, loc) }),
	}
	if next != 0 {
		page.NextCursor = strconv.FormatInt(next, 10)
	}
	c.JSON(http.StatusOK, page)
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
