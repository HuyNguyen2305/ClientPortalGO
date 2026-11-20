package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"clientportal/internal/dto"
	"clientportal/internal/service"
)

type serviceHandler struct {
	svc *service.Catalog
}

func (h serviceHandler) create(c *gin.Context) {
	var req dto.ServiceRequest
	if err := bindJSON(c, &req); err != nil {
		RespondError(c, err)
		return
	}
	in, err := req.ToModel()
	if err != nil {
		RespondError(c, err)
		return
	}
	s, err := h.svc.Create(c.Request.Context(), in)
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, dto.FromService(s))
}

// list shows customers the active templates.
func (h serviceHandler) list(c *gin.Context) { h.listStatus(c, "") }

// listAll is the admin list; ?status= picks active (default), archived or deleted.
func (h serviceHandler) listAll(c *gin.Context) { h.listStatus(c, c.Query("status")) }

func (h serviceHandler) listStatus(c *gin.Context, status string) {
	ss, err := h.svc.List(c.Request.Context(), status)
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, dto.Page[dto.ServiceResponse]{Data: dto.MapSlice(ss, dto.FromService)})
}

// get shows customers an active template.
func (h serviceHandler) get(c *gin.Context) {
	id, err := PathID(c)
	if err != nil {
		RespondError(c, err)
		return
	}
	s, err := h.svc.GetActive(c.Request.Context(), id)
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
	in, err := req.ToModel()
	if err != nil {
		RespondError(c, err)
		return
	}
	s, err := h.svc.Update(c.Request.Context(), id, in)
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, dto.FromService(s))
}

func (h serviceHandler) archive(c *gin.Context) {
	id, err := PathID(c)
	if err != nil {
		RespondError(c, err)
		return
	}
	if err := h.svc.Archive(c.Request.Context(), id); err != nil {
		RespondError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
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
