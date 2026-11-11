package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"clientportal/internal/dto"
	"clientportal/internal/service"
)

type customerHandler struct {
	svc *service.CustomerService
}

func (h customerHandler) create(c *gin.Context) {
	var req dto.CustomerRequest
	if err := bindJSON(c, &req); err != nil {
		RespondError(c, err)
		return
	}
	cust, err := h.svc.Create(c.Request.Context(), req.Name, req.Email, req.Phone)
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, dto.FromCustomer(cust))
}

func (h customerHandler) get(c *gin.Context) {
	id, err := PathID(c)
	if err != nil {
		RespondError(c, err)
		return
	}
	cust, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, dto.FromCustomer(cust))
}

func (h customerHandler) list(c *gin.Context) {
	limit, err := dto.ParseLimit(c.Query("limit"))
	if err != nil {
		RespondError(c, err)
		return
	}
	var afterID int64
	if raw := c.Query("cursor"); raw != "" {
		if afterID, err = positiveInt64("cursor", raw); err != nil {
			RespondError(c, err)
			return
		}
	}
	items, next, err := h.svc.List(c.Request.Context(), afterID, limit)
	if err != nil {
		RespondError(c, err)
		return
	}
	page := dto.Page[dto.CustomerResponse]{Data: dto.MapSlice(items, dto.FromCustomer)}
	if next != 0 {
		page.NextCursor = strconv.FormatInt(next, 10)
	}
	c.JSON(http.StatusOK, page)
}

func (h customerHandler) update(c *gin.Context) {
	id, err := PathID(c)
	if err != nil {
		RespondError(c, err)
		return
	}
	var req dto.CustomerRequest
	if err := bindJSON(c, &req); err != nil {
		RespondError(c, err)
		return
	}
	cust, err := h.svc.Update(c.Request.Context(), id, req.Name, req.Email, req.Phone)
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, dto.FromCustomer(cust))
}

func (h customerHandler) delete(c *gin.Context) {
	id, err := PathID(c)
	if err != nil {
		RespondError(c, err)
		return
	}
	if err := h.svc.Delete(c.Request.Context(), id); err != nil {
		RespondError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
