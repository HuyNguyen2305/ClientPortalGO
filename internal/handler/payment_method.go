package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"clientportal/internal/dto"
	"clientportal/internal/middleware"
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

func (h paymentMethodHandler) delete(c *gin.Context) {
	if err := h.svc.Delete(c.Request.Context(), middleware.CustomerID(c), c.Param("id")); err != nil {
		RespondError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
