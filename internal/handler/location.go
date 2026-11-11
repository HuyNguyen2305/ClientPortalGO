package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"clientportal/internal/dto"
	"clientportal/internal/middleware"
	"clientportal/internal/service"
)

type locationHandler struct {
	svc *service.LocationService
}

func (h locationHandler) create(c *gin.Context) {
	var req dto.LocationRequest
	if err := bindJSON(c, &req); err != nil {
		RespondError(c, err)
		return
	}
	l, err := h.svc.Create(c.Request.Context(), middleware.CustomerID(c), req.ToModel())
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, dto.FromLocation(l))
}

func (h locationHandler) list(c *gin.Context) {
	ls, err := h.svc.List(c.Request.Context(), middleware.CustomerID(c))
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, dto.Page[dto.LocationResponse]{Data: dto.MapSlice(ls, dto.FromLocation)})
}

func (h locationHandler) get(c *gin.Context) {
	id, err := PathID(c)
	if err != nil {
		RespondError(c, err)
		return
	}
	l, err := h.svc.Get(c.Request.Context(), middleware.CustomerID(c), id)
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, dto.FromLocation(l))
}

func (h locationHandler) update(c *gin.Context) {
	id, err := PathID(c)
	if err != nil {
		RespondError(c, err)
		return
	}
	var req dto.LocationRequest
	if err := bindJSON(c, &req); err != nil {
		RespondError(c, err)
		return
	}
	l, err := h.svc.Update(c.Request.Context(), middleware.CustomerID(c), id, req.ToModel())
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, dto.FromLocation(l))
}

func (h locationHandler) delete(c *gin.Context) {
	id, err := PathID(c)
	if err != nil {
		RespondError(c, err)
		return
	}
	if err := h.svc.Delete(c.Request.Context(), middleware.CustomerID(c), id); err != nil {
		RespondError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
