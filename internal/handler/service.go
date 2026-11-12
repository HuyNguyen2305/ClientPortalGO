package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"clientportal/internal/dto"
	"clientportal/internal/service"
)

// shortcut: admin routes have no authentication, same as X-Customer-ID; add admin auth before going public.
type serviceHandler struct {
	svc *service.Catalog
}

func (h serviceHandler) create(c *gin.Context) {
	var req dto.ServiceRequest
	if err := bindJSON(c, &req); err != nil {
		RespondError(c, err)
		return
	}
	s, err := h.svc.Create(c.Request.Context(), req.ToModel())
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, dto.FromService(s))
}

func (h serviceHandler) list(c *gin.Context) {
	ss, err := h.svc.List(c.Request.Context())
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, dto.Page[dto.ServiceResponse]{Data: dto.MapSlice(ss, dto.FromService)})
}

func (h serviceHandler) get(c *gin.Context) {
	id, err := PathID(c)
	if err != nil {
		RespondError(c, err)
		return
	}
	s, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, dto.FromService(s))
}

func (h serviceHandler) update(c *gin.Context) {
	id, err := PathID(c)
	if err != nil {
		RespondError(c, err)
		return
	}
	var req dto.ServiceRequest
	if err := bindJSON(c, &req); err != nil {
		RespondError(c, err)
		return
	}
	s, err := h.svc.Update(c.Request.Context(), id, req.ToModel())
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, dto.FromService(s))
}

func (h serviceHandler) delete(c *gin.Context) {
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
