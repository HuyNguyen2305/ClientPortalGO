package handler

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"clientportal/internal/dto"
	"clientportal/internal/middleware"
	"clientportal/internal/model"
	"clientportal/internal/service"
)

type paymentMethodHandler struct {
	svc *service.PaymentMethodService
}

func (h paymentMethodHandler) setupIntent(c *gin.Context) {
	secret, err := h.svc.SetupIntent(c.Request.Context(), middleware.CustomerID(c))
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, dto.SetupIntentResponse{ClientSecret: secret})
}

func (h paymentMethodHandler) list(c *gin.Context) {
	cards, err := h.svc.List(c.Request.Context(), middleware.CustomerID(c))
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, dto.Page[dto.CardResponse]{Data: dto.MapSlice(cards, dto.FromCard)})
}

// payInvoice serves POST /invoices/:id/pay. The Idempotency-Key header is required: it is what keeps a retry from charging twice.
func (h paymentMethodHandler) payInvoice(c *gin.Context) {
	id, err := PathID(c)
	if err != nil {
		RespondError(c, err)
		return
	}
	var req dto.PayRequest
	if err := bindJSON(c, &req); err != nil {
		RespondError(c, err)
		return
	}
	key := c.GetHeader("Idempotency-Key")
	if key == "" || len(key) > maxIdempotencyKeyLen {
		RespondError(c, fmt.Errorf("%w: Idempotency-Key is required, up to %d characters", model.ErrValidation, maxIdempotencyKeyLen))
		return
	}
	doc, err := h.svc.PayInvoice(c.Request.Context(), middleware.CustomerID(c), id, req.PaymentMethodID, key)
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, dto.FromDocument(doc, true))
}

func (h paymentMethodHandler) delete(c *gin.Context) {
	if err := h.svc.Delete(c.Request.Context(), middleware.CustomerID(c), c.Param("id")); err != nil {
		RespondError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
