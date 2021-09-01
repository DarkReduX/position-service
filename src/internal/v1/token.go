package v1

import (
	"github.com/dgrijalva/jwt-go/v4"
	log "github.com/sirupsen/logrus"
)

func GenerateUserToken(username string) (string, error) {
	log.WithFields(log.Fields{
		"user": username,
	}).Info("Generate user token")

	claims := jwt.StandardClaims{
		Subject:   username,
		ExpiresAt: jwt.Now(),
	}
	tmpToken, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(""))
	return tmpToken, err
}
