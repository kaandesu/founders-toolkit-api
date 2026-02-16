package auth

import (
	"errors"
	"fmt"
	"founders-toolkit-api/models"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type AuthClaims struct {
	jwt.RegisteredClaims
}

var hmacSecret = []byte(os.Getenv("HMAC_SECRET"))

func generateToken(id int, expiresAt time.Time) *jwt.Token {
	return jwt.NewWithClaims(jwt.SigningMethodHS256, AuthClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   fmt.Sprintf("%d", id),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	})
}

func GenerateAccessTokenString(user models.User) (string, error) {
	token := generateToken(int(user.ID), time.Now().Add(15*time.Minute))
	return token.SignedString(hmacSecret)
}

func GenerateRefreshTokenString(user models.User) (string, error) {
	token := generateToken(int(user.ID), time.Now().Add(time.Hour*24*30))
	return token.SignedString(hmacSecret)
}

func ParseToken(tokenString string) (*AuthClaims, error) {
	claims := &AuthClaims{}

	token, err := jwt.ParseWithClaims(tokenString, claims,
		func(t *jwt.Token) (any, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
			}
			return hmacSecret, nil
		})
	if err != nil {
		return nil, errors.Join(err, jwt.ErrTokenNotValidYet)
	}

	if !token.Valid {
		return nil, jwt.ErrTokenNotValidYet
	}

	if claims.Subject == "" {
		return nil, jwt.ErrTokenInvalidSubject
	}

	return claims, nil
}
