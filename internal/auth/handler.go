package auth

import (
	"founders-toolkit-api/internal/database"
	"founders-toolkit-api/internal/response"
	"founders-toolkit-api/models"
	"net/http"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

const (
	ErrUserExists      = "User already exists"
	ErrUserNotFound    = "User does not exist"
	ErrInvalidPassword = "Invalid password"
	ErrTokenFailure    = "Failed to create token"
)

func SignUp(db *database.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			Email    string `json:"email" binding:"required,email"`
			Password string `json:"password" binding:"required,min=6"`
		}

		if err := c.ShouldBindJSON(&body); err != nil {
			response.Respond(c, http.StatusBadRequest, err.Error(), nil)
			return
		}

		hashedPassword, err := bcrypt.GenerateFromPassword([]byte(body.Password), bcrypt.DefaultCost)
		if err != nil {
			response.Respond(c, http.StatusInternalServerError, ErrTokenFailure, nil)
			return
		}

		user := &models.User{
			Email:    body.Email,
			Password: string(hashedPassword),
		}

		if err := db.CreateUser(user); err != nil {
			response.Respond(c, http.StatusConflict, ErrUserExists, nil)
			return
		}

		accessToken, err := GenerateAccessTokenString(*user)
		if err != nil {
			response.Respond(c, http.StatusBadRequest, ErrTokenFailure, nil)
			return
		}

		refreshToken, err := GenerateRefreshTokenString(*user)
		if err != nil {
			response.Respond(c, http.StatusBadRequest, ErrTokenFailure, nil)
			return
		}

		response.Respond(c, http.StatusCreated, "User created successfully",
			gin.H{
				"access_token":  accessToken,
				"refresh_token": refreshToken,
				"expires_in":    900, // 15mins
			})
	}
}

func Login(db *database.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			Email    string `json:"email"`
			Password string `json:"password"`
		}

		if err := c.ShouldBindJSON(&body); err != nil {
			response.Respond(c, http.StatusBadRequest, err.Error(), nil)
			return
		}

		user, err := db.FindUserByEmail(body.Email)
		if err != nil {
			response.Respond(c, http.StatusInternalServerError, "Something went wrong", nil)
			return
		}
		if user.ID == 0 {
			response.Respond(c, http.StatusNotFound, ErrUserNotFound, nil)
			return
		}

		err = bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(body.Password))
		if err != nil {
			response.Respond(c, http.StatusUnauthorized, ErrInvalidPassword, nil)
			return
		}

		accessToken, err := GenerateAccessTokenString(user)
		if err != nil {
			response.Respond(c, http.StatusBadRequest, ErrTokenFailure, nil)
			return
		}

		refreshToken, err := GenerateRefreshTokenString(user)
		if err != nil {
			response.Respond(c, http.StatusBadRequest, ErrTokenFailure, nil)
			return
		}

		response.Respond(c, http.StatusOK, "Login successful",
			gin.H{
				"access_token":  accessToken,
				"refresh_token": refreshToken,
				"expires_in":    900, // 15mins
			})
	}
}

func RefreshAccessToken(db *database.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		var body struct {
			RefreshToken string `json:"refresh_token" binding:"required"`
		}

		if err := c.ShouldBindJSON(&body); err != nil {
			response.Respond(c, http.StatusBadRequest, err.Error(), nil)
			return
		}

		claims, err := ParseToken(body.RefreshToken)
		if err != nil {
			response.Respond(c, http.StatusUnauthorized, err.Error(), nil)
			return
		}

		userId := claims.Subject
		user, err := db.FindUserById(userId)
		if err != nil {
			response.Respond(c, http.StatusInternalServerError, err.Error(), nil)
			return
		}

		accessToken, err := GenerateAccessTokenString(user)
		if err != nil {
			response.Respond(c, http.StatusInternalServerError, ErrTokenFailure, nil)
		}
		response.Respond(c, http.StatusOK, "Token Refreshed",
			gin.H{
				"access_token": accessToken,
				"expires_in":   900,
			})
	}
}

const (
	ErrIncorrectCurrentPassword = "Current password is incorrect"
	ErrPasswordUpdateFailed     = "Password could not be updated"
	ErrHashFailure              = "Error while changing the password"
)

const (
	MsgPasswordChanged = "Password updated successfully"
)

func ChangePassword(db *database.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		_user, ok := c.Get("user")
		user := _user.(models.User)

		if !ok {
			response.Respond(c, http.StatusInternalServerError, "kullanici bulunamadi", nil)
			return
		}

		var body struct {
			CurrentPassword string `json:"current_password" binding:"required,min=4"`
			NewPassword     string `json:"new_password" binding:"required,min=4"`
		}

		if err := c.ShouldBindJSON(&body); err != nil {
			response.Respond(c, http.StatusBadRequest, err.Error(), nil)
			return
		}

		err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(body.CurrentPassword))
		if err != nil {
			response.Respond(c, http.StatusBadRequest, ErrIncorrectCurrentPassword, nil)
			return
		}

		hashedPassword, err := bcrypt.GenerateFromPassword([]byte(body.NewPassword), bcrypt.DefaultCost)
		if err != nil {
			response.Respond(c, http.StatusInternalServerError, ErrHashFailure, nil)
			return
		}

		if err := db.DB.Table("users").Where("id = ?", user.ID).Update("password", hashedPassword).Error; err != nil {
			response.Respond(c, http.StatusInternalServerError, ErrPasswordUpdateFailed, err.Error())
			return
		}

		response.Respond(c, http.StatusOK, MsgPasswordChanged, nil)
	}
}

func Logout(c *gin.Context) {
	c.SetCookie("Authorization", "", -1, "/", "", false, true)
	response.Respond(c, http.StatusOK, "Logged out successfully", nil)
}

func Validate(c *gin.Context) {
	response.Respond(c, http.StatusOK, "Logged in", nil)
}
