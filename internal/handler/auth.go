package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"clientportal/internal/dto"
	"clientportal/internal/middleware"
	"clientportal/internal/service"
)

type authHandler struct {
	svc *service.AuthService
}

func (h authHandler) register(c *gin.Context) {
	var req dto.RegisterRequest
	if err := bindJSON(c, &req); err != nil {
		RespondError(c, err)
		return
	}
	cust, token, expires, err := h.svc.Register(c.Request.Context(), req.Name, req.Email, req.Phone, req.Password)
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusCreated, dto.FromAuth(cust, token, expires))
}

func (h authHandler) login(c *gin.Context) {
	var req dto.LoginRequest
	if err := bindJSON(c, &req); err != nil {
		RespondError(c, err)
		return
	}
	cust, token, expires, err := h.svc.Login(c.Request.Context(), req.Email, req.Password)
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, dto.FromAuth(cust, token, expires))
}

// requestReset answers 202 whether or not the address is registered.
func (h authHandler) requestReset(c *gin.Context) {
	var req dto.RequestResetRequest
	if err := bindJSON(c, &req); err != nil {
		RespondError(c, err)
		return
	}
	if err := h.svc.RequestReset(c.Request.Context(), req.Email); err != nil {
		RespondError(c, err)
		return
	}
	c.Status(http.StatusAccepted)
}

func (h authHandler) confirmReset(c *gin.Context) {
	var req dto.ConfirmResetRequest
	if err := bindJSON(c, &req); err != nil {
		RespondError(c, err)
		return
	}
	if err := h.svc.ConfirmReset(c.Request.Context(), req.Token, req.NewPassword); err != nil {
		RespondError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h authHandler) logout(c *gin.Context) {
	if err := h.svc.Logout(c.Request.Context(), middleware.Token(c)); err != nil {
		RespondError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h authHandler) changePassword(c *gin.Context) {
	var req dto.ChangePasswordRequest
	if err := bindJSON(c, &req); err != nil {
		RespondError(c, err)
		return
	}
	err := h.svc.ChangePassword(c.Request.Context(), middleware.CustomerID(c), middleware.Token(c), req.OldPassword, req.NewPassword)
	if err != nil {
		RespondError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
