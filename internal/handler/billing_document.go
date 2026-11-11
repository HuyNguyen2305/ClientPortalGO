package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"clientportal/internal/dto"
	"clientportal/internal/middleware"
	"clientportal/internal/model"
	"clientportal/internal/service"
)

// BillingDocumentHandler serves one parent type; the router builds one per type.
type BillingDocumentHandler struct {
	svc *service.BillingDocumentService
	pt  model.ParentType
}

// NewBillingDocumentHandler returns a handler fixed to parent type pt.
func NewBillingDocumentHandler(svc *service.BillingDocumentService, pt model.ParentType) *BillingDocumentHandler {
	return &BillingDocumentHandler{svc: svc, pt: pt}
}

func (h *BillingDocumentHandler) bindInput(c *gin.Context) (model.DocumentInput, error) {
	var req dto.DocumentRequest
	if err := bindJSON(c, &req); err != nil {
		return model.DocumentInput{}, err
	}
	return req.ToInput()
}

// List serves GET / with location_id, status, limit and cursor filters.
func (h *BillingDocumentHandler) List(c *gin.Context) {
	f := model.DocumentFilter{CustomerID: middleware.CustomerID(c), ParentType: h.pt}
	var err error
	if f.Limit, err = dto.ParseLimit(c.Query("limit")); err != nil {
		RespondError(c, err)
		return
	}
	if f.LocationID, err = QueryInt64(c, "location_id"); err != nil {
		RespondError(c, err)
		return
	}
	if raw, ok := c.GetQuery("status"); ok {
		s := model.Status(raw)
		f.Status = &s
	}
	if raw, ok := c.GetQuery("cursor"); ok {
		cur, err := model.DecodeCursor(raw)
		if err != nil {
			RespondError(c, err)
			return
		}
		f.After = &cur
	}
	docs, next, err := h.svc.List(c.Request.Context(), f)
	if err != nil {
		RespondError(c, err)
		return
	}
	page := dto.Page[dto.DocumentResponse]{Data: dto.MapSlice(docs, dto.ListItem)}
	if next != nil {
		page.NextCursor = next.Encode()
	}
	c.JSON(http.StatusOK, page)
}

// Create serves POST /.
func (h *BillingDocumentHandler) Create(c *gin.Context) {
	in, err := h.bindInput(c)
	if err != nil {
		RespondError(c, err)
		return
	}
	doc, err := h.svc.Create(c.Request.Context(), middleware.CustomerID(c), h.pt, in)
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, dto.FromDocument(doc, true))
}

// Get serves GET /:id.
func (h *BillingDocumentHandler) Get(c *gin.Context) {
	id, err := PathID(c)
	if err != nil {
		RespondError(c, err)
		return
	}
	doc, err := h.svc.Get(c.Request.Context(), middleware.CustomerID(c), h.pt, id)
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, dto.FromDocument(doc, true))
}

// Update serves PUT /:id.
func (h *BillingDocumentHandler) Update(c *gin.Context) {
	id, err := PathID(c)
	if err != nil {
		RespondError(c, err)
		return
	}
	in, err := h.bindInput(c)
	if err != nil {
		RespondError(c, err)
		return
	}
	doc, err := h.svc.Update(c.Request.Context(), middleware.CustomerID(c), h.pt, id, in)
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, dto.FromDocument(doc, true))
}

// Transition returns a handler for POST /:id/<action> that moves the document to status `to`.
func (h *BillingDocumentHandler) Transition(to model.Status) gin.HandlerFunc {
	return func(c *gin.Context) {
		id, err := PathID(c)
		if err != nil {
			RespondError(c, err)
			return
		}
		doc, err := h.svc.Transition(c.Request.Context(), middleware.CustomerID(c), h.pt, id, to)
		if err != nil {
			RespondError(c, err)
			return
		}
		c.JSON(http.StatusOK, dto.FromDocument(doc, true))
	}
}

// Delete serves DELETE /:id.
func (h *BillingDocumentHandler) Delete(c *gin.Context) {
	id, err := PathID(c)
	if err != nil {
		RespondError(c, err)
		return
	}
	if err := h.svc.Delete(c.Request.Context(), middleware.CustomerID(c), h.pt, id); err != nil {
		RespondError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
